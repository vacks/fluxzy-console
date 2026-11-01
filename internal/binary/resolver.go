package binary

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	fluxzyversion "github.com/vacks/fluxzy-console/internal/fluxzy/version"
	"github.com/vacks/fluxzy-console/internal/pathutil"
)

type Source string

const (
	SourceBundled Source = "bundled"
)

type Info struct {
	Path           string             `json:"path"`
	Source         Source             `json:"source"`
	Version        string             `json:"version"`
	BaseVersion    string             `json:"baseVersion"`
	Commit         string             `json:"commit"`
	ReleaseTag     string             `json:"releaseTag"`
	BundledRelease string             `json:"bundledRelease"`
	CheckedAt      int64              `json:"checkedAt"`
	VersionInfo    fluxzyversion.Info `json:"-"`
}

// Resolver exposes the fluxzy CLI bundled in the image. It is the only CLI the
// console uses, so the body-transform and hot-reload patches are always active.
type Resolver struct {
	bundledDir string
	bundledRel string
	versionTTL time.Duration
	mu         sync.Mutex
	cached     *Info
	cachedAt   time.Time
}

func New(bundledDir, bundledRelease string) *Resolver {
	return &Resolver{
		bundledDir: bundledDir,
		bundledRel: bundledRelease,
		versionTTL: 30 * time.Second,
	}
}

func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.cachedAt = time.Time{}
	r.mu.Unlock()
}

func (r *Resolver) Resolve(ctx context.Context) (Info, error) {
	r.mu.Lock()
	if r.cached != nil && time.Since(r.cachedAt) < r.versionTTL {
		info := *r.cached
		r.mu.Unlock()
		return info, nil
	}
	r.mu.Unlock()

	info, err := r.resolveUncached(ctx)
	if err != nil {
		return Info{}, err
	}
	info.CheckedAt = time.Now().UnixMilli()
	info.BundledRelease = r.bundledRel

	r.mu.Lock()
	r.cached = &info
	r.cachedAt = time.Now()
	r.mu.Unlock()
	return info, nil
}

func (r *Resolver) resolveUncached(ctx context.Context) (Info, error) {
	path := filepath.Join(r.bundledDir, "fluxzy")
	if !pathutil.SafeUnder(path, r.bundledDir) {
		return Info{}, fmt.Errorf("bundled CLI path escapes %s", r.bundledDir)
	}
	if err := executable(path); err != nil {
		return Info{}, fmt.Errorf("bundled fluxzy CLI %s: %w", path, err)
	}
	ver, err := probeVersion(ctx, path)
	if err != nil {
		return Info{}, fmt.Errorf("probe bundled fluxzy CLI %s: %w", path, err)
	}
	info := Info{
		Path:        path,
		Source:      SourceBundled,
		Version:     ver.Raw,
		BaseVersion: ver.Base(),
		Commit:      ver.Commit,
		ReleaseTag:  ver.ReleaseTag,
		VersionInfo: ver,
	}
	if info.ReleaseTag == "" {
		info.ReleaseTag = r.bundledRel
	}
	return info, nil
}

func (r *Resolver) Path(ctx context.Context) (string, error) {
	info, err := r.Resolve(ctx)
	if err != nil {
		return "", err
	}
	return info.Path, nil
}

func (r *Resolver) Version(ctx context.Context) (fluxzyversion.Info, error) {
	info, err := r.Resolve(ctx)
	if err != nil {
		return fluxzyversion.Info{}, err
	}
	return info.VersionInfo, nil
}

func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory")
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("is not executable")
	}
	return nil
}

func probeVersion(ctx context.Context, path string) (fluxzyversion.Info, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "--version")
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = filepath.Dir(path)
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/bin:/usr/bin:/bin"
	}
	cmd.Env = []string{
		"PATH=" + pathEnv,
		"HOME=" + os.TempDir(),
		"TMPDIR=" + os.TempDir(),
		"LANG=C",
		"DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fluxzyversion.Info{}, fmt.Errorf("%w: %s", err, truncate(string(out), 512))
	}
	return fluxzyversion.ParseCLIOutput(string(out))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
