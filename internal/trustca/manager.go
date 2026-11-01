// Package trustca manages the root certificate fluxzy uses for TLS
// interception. Fluxzy accepts a PKCS#12 file through the
// FLUXZY_ROOT_CERTIFICATE environment variable, so the console stores the
// active certificate in its persistent data directory and restarts fluxzy
// after a change.
package trustca

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const MaxUploadBytes = 4 << 20

type PathResolver interface {
	Path(ctx context.Context) (string, error)
}

type Restarter interface {
	Restart(ctx context.Context, reason string) error
}

type Info struct {
	Source            string `json:"source"`
	Subject           string `json:"subject"`
	Issuer            string `json:"issuer"`
	CommonName        string `json:"commonName"`
	SerialNumber      string `json:"serialNumber"`
	NotBefore         int64  `json:"notBefore"`
	NotAfter          int64  `json:"notAfter"`
	FingerprintSHA256 string `json:"fingerprintSha256"`
	Path              string `json:"path"`
	Size              int64  `json:"size"`
	UpdatedAt         int64  `json:"updatedAt"`
	DaysRemaining     int    `json:"daysRemaining"`
}

type Manager struct {
	resolver PathResolver
	sup      Restarter
	dir      string
	log      *slog.Logger
	mu       sync.Mutex
}

func New(resolver PathResolver, dir string, sup Restarter, log *slog.Logger) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Manager{resolver: resolver, sup: sup, dir: dir, log: log}, nil
}

func (m *Manager) ActivePath() string {
	return filepath.Join(m.dir, "rootca.pfx")
}

func (m *Manager) BackupPath() string {
	return filepath.Join(m.dir, "rootca.pfx.bak")
}

// ApplyEnv makes all fluxzy subprocesses use the managed CA. If no custom CA
// exists, the inherited override is removed so fluxzy uses its embedded CA.
func (m *Manager) ApplyEnv() error {
	path := m.ActivePath()
	if _, err := os.Stat(path); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := m.exportCertificate(ctx, path); err != nil {
			return fmt.Errorf("自定义 CA 无法读取: %w", err)
		}
		return os.Setenv("FLUXZY_ROOT_CERTIFICATE", path)
	}
	return os.Unsetenv("FLUXZY_ROOT_CERTIFICATE")
}

// Quarantine moves an unreadable active CA aside so startup can safely fall
// back to the embedded CA without repeatedly failing.
func (m *Manager) Quarantine() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := m.ActivePath()
	if _, err := os.Stat(active); err != nil {
		return nil
	}
	return os.Rename(active, active+".invalid")
}

func (m *Manager) Status(ctx context.Context) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(ctx)
}

func (m *Manager) statusLocked(ctx context.Context) (Info, error) {
	active := m.ActivePath()
	caPath := ""
	source := "embedded"
	var size, updated int64
	if st, err := os.Stat(active); err == nil {
		caPath = active
		source = "custom"
		size = st.Size()
		updated = st.ModTime().UnixMilli()
	}
	cert, err := m.exportCertificate(ctx, caPath)
	if err != nil {
		return Info{}, err
	}
	return infoFromCert(cert, source, caPath, size, updated), nil
}

func (m *Manager) Upload(ctx context.Context, src io.Reader) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tmp, err := os.CreateTemp(m.dir, "upload-*.pfx")
	if err != nil {
		return Info{}, err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	limited := io.LimitReader(src, MaxUploadBytes+1)
	n, err := io.Copy(tmp, limited)
	if err != nil {
		return Info{}, err
	}
	if n > MaxUploadBytes {
		return Info{}, fmt.Errorf("CA 文件超过 %d MiB 限制", MaxUploadBytes>>20)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return Info{}, err
	}
	if err := tmp.Close(); err != nil {
		return Info{}, err
	}
	if _, err := m.validateCertificate(ctx, tmpPath); err != nil {
		m.log.Warn("validate uploaded CA", "error", err)
		return Info{}, fmt.Errorf("CA 文件无效：请确认是有效的 PKCS#12 (.pfx/.p12)，且没有密码保护")
	}
	info, err := m.installLocked(ctx, tmpPath)
	if err == nil {
		cleanup = false
	}
	return info, err
}

func (m *Manager) Regenerate(ctx context.Context, commonName string) (Info, error) {
	commonName = strings.TrimSpace(commonName)
	if commonName == "" {
		commonName = "fluxzy-console ROOT CA"
	}
	if len(commonName) > 128 {
		return Info{}, fmt.Errorf("CA 名称不能超过 128 个字符")
	}
	if strings.HasPrefix(commonName, "-") || strings.Contains(commonName, "=") {
		return Info{}, fmt.Errorf("CA 名称不能以 '-' 开头或包含 '='")
	}
	for _, r := range commonName {
		if unicode.IsControl(r) {
			return Info{}, fmt.Errorf("CA 名称不能包含控制字符")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	tmp, err := os.CreateTemp(m.dir, "generated-*.pfx")
	if err != nil {
		return Info{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := m.runCLI(ctx, "", "cert", "create", tmpPath, commonName); err != nil {
		return Info{}, err
	}
	if _, err := m.validateCertificate(ctx, tmpPath); err != nil {
		return Info{}, fmt.Errorf("生成 CA 后校验失败: %w", err)
	}
	info, err := m.installLocked(ctx, tmpPath)
	if err == nil {
		cleanup = false
	}
	return info, err
}

// Reset removes the custom CA and restores fluxzy's embedded CA.
func (m *Manager) Reset(ctx context.Context) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(m.ActivePath()); err != nil {
		if os.IsNotExist(err) {
			return m.statusLocked(ctx)
		}
		return Info{}, err
	}
	if err := m.backupActive(); err != nil {
		return Info{}, err
	}
	if err := m.ApplyEnv(); err != nil {
		_ = m.restoreBackup()
		_ = m.ApplyEnv()
		return Info{}, err
	}
	if err := m.restart(ctx, "ca:reset-embedded"); err != nil {
		_ = m.restoreBackup()
		_ = m.ApplyEnv()
		_ = m.restart(ctx, "ca:reset-rollback")
		return Info{}, fmt.Errorf("恢复内置 CA 后 fluxzy 启动失败: %w", err)
	}
	_ = os.Remove(m.BackupPath())
	m.log.Info("restored embedded fluxzy CA")
	info, err := m.statusLocked(ctx)
	if err != nil {
		m.log.Warn("read CA status after reset", "error", err)
		return Info{Source: "embedded"}, nil
	}
	return info, nil
}

func (m *Manager) installLocked(ctx context.Context, tempPath string) (Info, error) {
	if err := m.backupActive(); err != nil {
		return Info{}, err
	}
	if err := os.Rename(tempPath, m.ActivePath()); err != nil {
		_ = m.restoreBackup()
		_ = m.ApplyEnv()
		return Info{}, err
	}
	if err := m.ApplyEnv(); err != nil {
		_ = os.Remove(m.ActivePath())
		_ = m.restoreBackup()
		return Info{}, err
	}
	if err := m.restart(ctx, "ca:apply"); err != nil {
		_ = os.Remove(m.ActivePath())
		_ = m.restoreBackup()
		_ = m.ApplyEnv()
		_ = m.restart(ctx, "ca:apply-rollback")
		return Info{}, fmt.Errorf("CA 已更换但 fluxzy 启动失败，已尝试恢复: %w", err)
	}
	m.log.Info("applied custom fluxzy CA", "path", m.ActivePath())
	info, err := m.statusLocked(ctx)
	if err != nil {
		m.log.Warn("read CA status after install", "error", err)
		return Info{Source: "custom", Path: m.ActivePath()}, nil
	}
	return info, nil
}

func (m *Manager) backupActive() error {
	active := m.ActivePath()
	if _, err := os.Stat(active); err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(m.BackupPath())
			return nil
		}
		return err
	}
	_ = os.Remove(m.BackupPath())
	return os.Rename(active, m.BackupPath())
}

func (m *Manager) restoreBackup() error {
	backup := m.BackupPath()
	if _, err := os.Stat(backup); err != nil {
		return nil
	}
	_ = os.Remove(m.ActivePath())
	return os.Rename(backup, m.ActivePath())
}

func (m *Manager) restart(ctx context.Context, reason string) error {
	if m.sup == nil {
		return nil
	}
	base := ctx
	if base == nil {
		base = context.Background()
	}
	restartCtx, cancel := context.WithTimeout(context.WithoutCancel(base), 90*time.Second)
	defer cancel()
	return m.sup.Restart(restartCtx, reason)
}

func (m *Manager) validateCertificate(ctx context.Context, pfxPath string) (*x509.Certificate, error) {
	cert, err := m.exportCertificate(ctx, pfxPath)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, fmt.Errorf("上传的证书不是有效的 CA 证书")
	}
	if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, fmt.Errorf("上传的 CA 证书缺少证书签发用途")
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, fmt.Errorf("上传的 CA 证书不在有效期内")
	}
	return cert, nil
}

func (m *Manager) exportCertificate(ctx context.Context, caPath string) (*x509.Certificate, error) {
	tmp, err := os.CreateTemp(m.dir, "export-*.pem")
	if err != nil {
		return nil, err
	}
	out := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(out)
	defer os.Remove(out)
	if _, err := m.runCLI(ctx, caPath, "cert", "export", out); err != nil {
		return nil, err
	}
	return parseCertificate(out)
}

func (m *Manager) runCLI(ctx context.Context, caPath string, args ...string) (string, error) {
	if m.resolver == nil {
		return "", fmt.Errorf("fluxzy CLI 不可用")
	}
	bin, err := m.resolver.Path(ctx)
	if err != nil {
		return "", fmt.Errorf("fluxzy CLI 不可用: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = m.dir
	cmd.Env = caEnv(caPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func caEnv(caPath string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if strings.HasPrefix(key, "CONSOLE_") || key == "FLUXZY_ROOT_CERTIFICATE" {
			continue
		}
		out = append(out, entry)
	}
	if caPath != "" {
		out = append(out, "FLUXZY_ROOT_CERTIFICATE="+caPath)
	}
	return out
}

func parseCertificate(path string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("无法解析 CA 证书: %w", err)
	}
	return cert, nil
}

func infoFromCert(cert *x509.Certificate, source, path string, size, updated int64) Info {
	fingerprint := sha256.Sum256(cert.Raw)
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return Info{
		Source:            source,
		Subject:           cert.Subject.String(),
		Issuer:            cert.Issuer.String(),
		CommonName:        cert.Subject.CommonName,
		SerialNumber:      cert.SerialNumber.Text(16),
		NotBefore:         cert.NotBefore.UnixMilli(),
		NotAfter:          cert.NotAfter.UnixMilli(),
		FingerprintSHA256: hex.EncodeToString(fingerprint[:]),
		Path:              path,
		Size:              size,
		UpdatedAt:         updated,
		DaysRemaining:     days,
	}
}
