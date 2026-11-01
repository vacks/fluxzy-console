package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
)

var ErrInvalidRules = errors.New("规则校验未通过")

type Status struct {
	YAML             string                `json:"yaml"`
	Hash             string                `json:"hash"`
	Exists           bool                  `json:"exists"`
	IsDefault        bool                  `json:"isDefault"`
	Pending          bool                  `json:"pending"`
	ActiveSessionID  string                `json:"activeSessionId"`
	ActiveSince      int64                 `json:"activeSince"`
	ActiveRules      json.RawMessage       `json:"activeRules,omitempty"`
	ModifiedAt       int64                 `json:"modifiedAt"`
	Summary          []ParsedRule          `json:"summary"`
	Versions         []store.ConfigVersion `json:"versions"`
	ParseError       string                `json:"parseError,omitempty"`
	AppliedVersionID int64                 `json:"appliedVersionId"`
	AppliedHash      string                `json:"appliedHash"`
	AppliedAt        int64                 `json:"appliedAt"`
	AppliedSessionID string                `json:"appliedSessionId"`
	LastApplyError   string                `json:"lastApplyError"`
}

type Service struct {
	cfg     Config
	store   *store.Store
	capture *capture.Manager
	cli     *CLI
	sup     *supervisor.Supervisor
	log     *slog.Logger

	// opMu serializes file writes, validation-based apply, and rollback. The
	// rules file and its version bookkeeping must move together; a concurrent
	// Save during Apply could otherwise make fluxzy load bytes newer than the
	// hash recorded as applied.
	opMu   sync.Mutex
	mu     sync.Mutex
	schema Schema
}

type ApplyResult struct {
	Applied         bool                 `json:"applied"`
	HotReloaded     bool                 `json:"hotReloaded"`
	Unchanged       bool                 `json:"unchanged"`
	RolledBack      bool                 `json:"rolledBack"`
	Version         *store.ConfigVersion `json:"version,omitempty"`
	Validation      ValidationResult     `json:"validation"`
	ActiveSessionID string               `json:"activeSessionId"`
	Message         string               `json:"message"`
}

type Config struct {
	RulesFile  string
	StatusFile string
}

func NewService(rulesFile string, st *store.Store, mgr *capture.Manager, cli *CLI, sup *supervisor.Supervisor, log *slog.Logger) *Service {
	return &Service{
		cfg: Config{
			RulesFile:  rulesFile,
			StatusFile: filepath.Join(filepath.Dir(rulesFile), ".rules-status.json"),
		},
		store:   st,
		capture: mgr,
		cli:     cli,
		sup:     sup,
		log:     log,
		schema:  BuildSchema(),
	}
}

func (s *Service) Schema() Schema { return s.schema }

func (s *Service) CLIAvailable() bool { return s.cli != nil && s.cli.Available() }

func (s *Service) ExportCA(ctx context.Context, out string) error {
	if s.cli == nil {
		return fmt.Errorf("fluxzy CLI 不可用")
	}
	return s.cli.ExportCA(ctx, out)
}

func (s *Service) Pack(ctx context.Context, inputDir, out, format string) error {
	if s.cli == nil {
		return fmt.Errorf("fluxzy CLI 不可用")
	}
	return s.cli.Pack(ctx, inputDir, out, format)
}

func (s *Service) EnsureDefault(ctx context.Context) error {
	if _, err := os.Stat(s.cfg.RulesFile); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeAtomic(s.cfg.RulesFile, []byte(DefaultYAML), 0o640); err != nil {
		return err
	}
	if s.log != nil {
		s.log.Info("created default rules file", "path", s.cfg.RulesFile)
	}
	return nil
}

func (s *Service) Read() (string, os.FileInfo, error) {
	b, err := os.ReadFile(s.cfg.RulesFile)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(s.cfg.RulesFile)
	if err != nil {
		return string(b), nil, nil
	}
	return string(b), info, nil
}

func (s *Service) pendingPath() string {
	return s.cfg.RulesFile + ".pending"
}

// ReadCandidate returns the saved-but-not-applied rules when present, and
// otherwise the live rules. Save/Rollback must never modify the live file,
// because the bundled CLI watches it and would apply the draft immediately.
func (s *Service) ReadCandidate() (string, os.FileInfo, bool, error) {
	if b, err := os.ReadFile(s.pendingPath()); err == nil {
		info, _ := os.Stat(s.pendingPath())
		return string(b), info, true, nil
	} else if !os.IsNotExist(err) {
		return "", nil, false, err
	}
	text, info, err := s.Read()
	return text, info, false, err
}

func (s *Service) Validate(ctx context.Context, yamlText string) ValidationResult {
	if s.cli == nil {
		return ValidationResult{Valid: true, Skipped: true, Errors: []string{}, Warnings: []string{"未配置 fluxzy CLI，已跳过运行时校验"}}
	}
	return s.cli.Validate(ctx, yamlText)
}

func (s *Service) CurrentHash() string {
	yamlText, _, err := s.Read()
	if err != nil {
		return ""
	}
	return HashYAML(yamlText)
}

func FileHash(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return HashYAML(string(b))
}

func (s *Service) Render(model RuleModel) (string, error) {
	return Render(model)
}

func (s *Service) Parse(data []byte) (RuleModel, error) {
	return Parse(data)
}

func (s *Service) Save(ctx context.Context, yamlText, note string, user *store.User) (*store.ConfigVersion, ValidationResult, error) {
	result := s.Validate(ctx, yamlText)
	if !result.Valid {
		return nil, result, ErrInvalidRules
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.saveLocked(ctx, yamlText, note, user, result)
}

func (s *Service) saveLocked(ctx context.Context, yamlText, note string, user *store.User, result ValidationResult) (*store.ConfigVersion, ValidationResult, error) {
	// Keep the live file untouched. The CLI watches it and Save is only a
	// draft operation; Apply replaces the live file.
	oldPending, oldPendingErr := os.ReadFile(s.pendingPath())
	if err := writeAtomic(s.pendingPath(), []byte(yamlText), 0o640); err != nil {
		return nil, result, err
	}

	var authorID *int64
	authorName := ""
	if user != nil {
		id := user.ID
		authorID = &id
		authorName = user.Username
	}
	version, err := s.store.CreateConfigVersion(ctx, store.ConfigVersion{
		YAML:             yamlText,
		YAMLHash:         HashYAML(yamlText),
		AuthorID:         authorID,
		AuthorName:       authorName,
		Note:             note,
		ValidationStatus: "valid",
		Status:           "pending",
	})
	if err != nil {
		if oldPendingErr == nil {
			_ = writeAtomic(s.pendingPath(), oldPending, 0o640)
		} else {
			_ = os.Remove(s.pendingPath())
		}
		return nil, result, err
	}
	return version, result, nil
}

func (s *Service) Rollback(ctx context.Context, versionID int64, user *store.User) (*store.ConfigVersion, ValidationResult, error) {
	v, err := s.store.GetConfigVersion(ctx, versionID)
	if err != nil {
		return nil, ValidationResult{}, err
	}
	result := s.Validate(ctx, v.YAML)
	if !result.Valid {
		return nil, result, ErrInvalidRules
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.saveLocked(ctx, v.YAML, fmt.Sprintf("回滚到版本 #%d", v.ID), user, result)
}

// Apply validates the current rules file and asks the supervised CLI to
// hot-reload it. If the CLI cannot confirm the new hash in time, it falls back
// to restarting fluxzy (which creates a new capture session).
func (s *Service) Apply(ctx context.Context, expectedHash string, user *store.User) (*ApplyResult, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	yamlText, _, pending, err := s.ReadCandidate()
	if err != nil {
		return nil, err
	}
	hash := HashYAML(yamlText)
	if expectedHash != "" && expectedHash != hash {
		return nil, fmt.Errorf("规则文件已变化，请刷新后重试")
	}
	validation := s.Validate(ctx, yamlText)
	if !validation.Valid {
		return &ApplyResult{Validation: validation, Message: "规则校验未通过"}, ErrInvalidRules
	}
	if s.sup == nil {
		return nil, fmt.Errorf("fluxzy 进程监管不可用")
	}

	// The apply operation owns the fluxzy restart. A disconnected browser must
	// not cancel the restart halfway through.
	opCtx, opCancel := context.WithTimeout(context.WithoutCancel(ctx), 75*time.Second)
	defer opCancel()

	version, err := s.store.FindConfigVersionByHash(opCtx, hash)
	if errors.Is(err, store.ErrNotFound) {
		var authorID *int64
		authorName := ""
		if user != nil {
			id := user.ID
			authorID = &id
			authorName = user.Username
		}
		version, err = s.store.CreateConfigVersion(opCtx, store.ConfigVersion{
			YAML: yamlText, YAMLHash: hash, AuthorID: authorID, AuthorName: authorName,
			Note: "应用当前规则文件", ValidationStatus: "valid", Status: "pending",
		})
	}
	if err != nil {
		return nil, err
	}
	_ = s.store.SetConfigVersionStatus(opCtx, version.ID, "applying", "")

	liveText, _, liveErr := s.Read()
	if liveErr != nil && !os.IsNotExist(liveErr) {
		return nil, liveErr
	}
	liveHash := HashYAML(liveText)
	changed := liveErr != nil || liveHash != hash

	if !changed && !pending {
		activeID := ""
		if s.capture != nil {
			activeID = s.capture.ActiveSessionID()
		}
		if err := s.store.MarkConfigVersionApplied(opCtx, version.ID, activeID); err != nil {
			return nil, err
		}
		if err := writeAtomic(s.cfg.RulesFile+".applied", []byte(yamlText), 0o640); err != nil {
			s.log.Warn("write applied rules backup", "error", err)
		}
		version.Status = "applied"
		version.AppliedAt = time.Now().UnixMilli()
		version.AppliedSessionID = activeID
		return &ApplyResult{
			Applied: true, Unchanged: true, Version: version, Validation: validation,
			ActiveSessionID: activeID, Message: "规则已是当前版本。",
		}, nil
	}

	if changed {
		if err := writeAtomic(s.cfg.RulesFile, []byte(yamlText), 0o640); err != nil {
			return nil, err
		}
	}

	if hotReloaded, hotErr := s.tryHotReload(opCtx, hash); hotReloaded {
		activeID := ""
		if s.capture != nil {
			activeID = s.capture.ActiveSessionID()
		}
		if err := s.store.MarkConfigVersionApplied(opCtx, version.ID, activeID); err != nil {
			return nil, err
		}
		if err := writeAtomic(s.cfg.RulesFile+".applied", []byte(yamlText), 0o640); err != nil {
			s.log.Warn("write applied rules backup", "error", err)
		}
		_ = os.Remove(s.pendingPath())
		version.Status = "applied"
		version.AppliedAt = time.Now().UnixMilli()
		version.AppliedSessionID = activeID
		return &ApplyResult{
			Applied:         true,
			HotReloaded:     true,
			Version:         version,
			Validation:      validation,
			ActiveSessionID: activeID,
			Message:         "规则已热更新。",
		}, nil
	} else if hotErr != "" {
		rolledBack := s.restoreRules(opCtx, liveText)
		_ = s.store.MarkConfigVersionFailed(opCtx, version.ID, hotErr)
		return &ApplyResult{
			Version: version, Validation: validation, RolledBack: rolledBack,
			Message: "规则热更新失败: " + hotErr,
		}, errors.New(hotErr)
	}

	if err := s.sup.Restart(opCtx, "rules:"+hash); err != nil {
		rolledBack := s.restoreRules(opCtx, liveText)
		_ = s.store.MarkConfigVersionFailed(opCtx, version.ID, err.Error())
		return &ApplyResult{
			Version: version, Validation: validation, RolledBack: rolledBack,
			Message: "重启 fluxzy 失败: " + err.Error(),
		}, err
	}

	activeID := ""
	warning := ""
	if s.capture != nil {
		activeID = s.capture.WaitForExpectedActive(opCtx, 10*time.Second)
	}
	if activeID == "" {
		if st := s.sup.Status(); st.State == supervisor.StateRunning {
			// fluxzy accepted the rules and is running; capture discovery can
			// lag behind. Do not report a false apply failure.
			if s.capture != nil {
				activeID = s.capture.ActiveSessionID()
			}
			warning = "规则已应用，但尚未确认新会话；稍后会自动发现。"
		} else {
			msg := "fluxzy 已启动，但未找到活动会话"
			_ = s.restoreRules(opCtx, liveText)
			_ = s.store.MarkConfigVersionFailed(opCtx, version.ID, msg)
			return &ApplyResult{Version: version, Validation: validation, Message: msg}, errors.New(msg)
		}
	}
	if activeID != "" {
		if err := s.store.SetCaptureSessionRulesHash(opCtx, activeID, hash); err != nil {
			s.log.Warn("set applied rules hash", "session", activeID, "error", err)
		}
	}
	if err := s.store.MarkConfigVersionApplied(opCtx, version.ID, activeID); err != nil {
		return nil, err
	}
	if err := writeAtomic(s.cfg.RulesFile+".applied", []byte(yamlText), 0o640); err != nil {
		s.log.Warn("write applied rules backup", "error", err)
	}
	_ = os.Remove(s.pendingPath())
	version.Status = "applied"
	version.AppliedAt = time.Now().UnixMilli()
	version.AppliedSessionID = activeID
	message := "规则已应用；fluxzy 已重启并创建新会话"
	if warning != "" {
		message = warning
	}
	result := &ApplyResult{
		Applied:         true,
		Version:         version,
		Validation:      validation,
		ActiveSessionID: activeID,
		Message:         message,
	}
	return result, nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	yamlText, info, _, err := s.ReadCandidate()
	if err != nil {
		if os.IsNotExist(err) {
			return Status{Exists: false, Pending: true, YAML: DefaultYAML, Hash: HashYAML(DefaultYAML)}, nil
		}
		return Status{}, err
	}

	st := Status{
		YAML:      yamlText,
		Hash:      HashYAML(yamlText),
		Exists:    true,
		IsDefault: strings.TrimSpace(yamlText) == strings.TrimSpace(DefaultYAML),
		Pending:   true,
	}
	if info != nil {
		st.ModifiedAt = info.ModTime().UnixMilli()
	}

	parsed, parseErr := Parse([]byte(yamlText))
	if parseErr != nil {
		st.ParseError = parseErr.Error()
	} else {
		for _, r := range parsed.Rules {
			p := ParsedRule{Name: r.Name, Filter: r.Filter, Actions: r.Actions}
			p.FilterKind, _ = r.Filter["typeKind"].(string)
			for _, a := range r.Actions {
				k, _ := a["typeKind"].(string)
				p.ActionKinds = append(p.ActionKinds, k)
			}
			st.Summary = append(st.Summary, p)
		}
	}

	appliedHash := ""
	if s.capture != nil {
		activeID := s.capture.ActiveSessionID()
		st.ActiveSessionID = activeID
		if activeID != "" {
			if sess, ok := s.capture.GetSession(activeID); ok {
				st.ActiveSince = sess.Meta.CaptureDate.UnixMilli()
				if len(sess.Meta.CapturedSetting) > 0 {
					var setting struct {
						AlterationRules json.RawMessage `json:"alterationRules"`
					}
					if json.Unmarshal(sess.Meta.CapturedSetting, &setting) == nil {
						st.ActiveRules = setting.AlterationRules
					}
				}
			}
			if cs, csErr := s.store.GetCaptureSession(ctx, activeID); csErr == nil && cs.RulesHash != "" {
				appliedHash = cs.RulesHash
				st.AppliedHash = cs.RulesHash
				st.AppliedSessionID = activeID
			}
		}
	}

	versions, err := s.store.ListConfigVersions(ctx, 30)
	if err == nil {
		st.Versions = versions
		for _, v := range versions {
			if v.Status == "applied" {
				st.AppliedVersionID = v.ID
				st.AppliedAt = v.AppliedAt
				st.AppliedSessionID = v.AppliedSessionID
				st.LastApplyError = v.AppliedError
				// The latest applied config version is authoritative. A hot
				// reload applies without creating a session, so the active
				// session's stored rules hash can still describe the rules it
				// started with.
				appliedHash = v.YAMLHash
				st.AppliedHash = v.YAMLHash
				break
			}
		}
	}
	if appliedHash == "" {
		st.Pending = true
	} else {
		st.Pending = appliedHash != st.Hash
	}
	return st, nil
}

func (s *Service) restoreBackup(ctx context.Context) bool {
	// .applied is the last version fluxzy actually loaded successfully.
	old, err := os.ReadFile(s.cfg.RulesFile + ".applied")
	if err != nil {
		return false
	}
	if err := writeAtomic(s.cfg.RulesFile, old, 0o640); err != nil {
		s.log.Error("restore rules backup", "error", err)
		return false
	}
	if s.sup == nil {
		return true
	}
	if err := s.sup.Restart(ctx, "rules:restore"); err != nil {
		s.log.Error("restart after rules restore", "error", err)
		return false
	}
	return true
}

// restoreRules prefers the last successfully applied rules, then falls back to
// the live bytes captured before this Apply wrote the candidate.
func (s *Service) restoreRules(ctx context.Context, fallback string) bool {
	if s.restoreBackup(ctx) {
		return true
	}
	if fallback == "" {
		return false
	}
	if err := writeAtomic(s.cfg.RulesFile, []byte(fallback), 0o640); err != nil {
		s.log.Error("restore live rules", "error", err)
		return false
	}
	if s.sup == nil {
		return true
	}
	if err := s.sup.Restart(ctx, "rules:restore"); err != nil {
		s.log.Error("restart after rules restore", "error", err)
		return false
	}
	return true
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".rules-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type hotReloadStatus struct {
	Success   bool   `json:"success"`
	Hash      string `json:"hash"`
	AppliedAt int64  `json:"appliedAt"`
	Rules     int    `json:"rules"`
	Error     string `json:"error"`
}

// tryHotReload waits for the bundled CLI to report the rule file hash after an
// in-process Proxy.UpdateRules call. A missing status file or timeout means the
// running binary does not support hot reload, and the caller should restart.
func (s *Service) tryHotReload(ctx context.Context, expectedHash string) (bool, string) {
	if s.sup == nil || s.sup.Status().State != supervisor.StateRunning {
		return false, ""
	}
	if s.cfg.StatusFile == "" {
		return false, ""
	}
	if _, err := os.Stat(s.cfg.StatusFile); err != nil {
		return false, ""
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()

	for {
		if raw, err := os.ReadFile(s.cfg.StatusFile); err == nil {
			var st hotReloadStatus
			if json.Unmarshal(raw, &st) == nil && st.Hash == expectedHash {
				if st.Success {
					return true, ""
				}
				if st.Error == "" {
					st.Error = "fluxzy 拒绝了新的规则"
				}
				return false, st.Error
			}
		}
		select {
		case <-ctx.Done():
			return false, ""
		case <-deadline.C:
			return false, ""
		case <-ticker.C:
		}
	}
}
