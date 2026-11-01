package retention

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/store"
)

const (
	settingHotHours      = "retention.hot_hours"
	settingColdDays      = "retention.cold_days"
	settingSamplePercent = "retention.sample_percent"
)

// Settings is the effective retention policy. Values are read from the
// settings table (edited from the UI) with the built-in defaults below.
type Settings struct {
	HotHours      int `json:"hotHours"`
	ColdDays      int `json:"coldDays"`
	SamplePercent int `json:"samplePercent"`
}

func (s Settings) HotDuration() time.Duration {
	return time.Duration(s.HotHours) * time.Hour
}

func (s Settings) ColdDuration() time.Duration {
	return time.Duration(s.ColdDays) * 24 * time.Hour
}

func validateSettings(s Settings) error {
	if s.HotHours < 0 || s.HotHours > 8760 {
		return fmt.Errorf("热数据保留小时数必须在 0–8760 之间")
	}
	if s.ColdDays < 0 || s.ColdDays > 3650 {
		return fmt.Errorf("冷数据保留天数必须在 0–3650 之间")
	}
	if s.SamplePercent < 0 || s.SamplePercent > 100 {
		return fmt.Errorf("采样比例必须在 0–100 之间")
	}
	if s.HotHours > 0 && s.ColdDays > 0 && s.ColdDuration() < s.HotDuration() {
		return fmt.Errorf("冷数据保留时间不能短于热数据保留时间")
	}
	return nil
}

// Manager applies the hot/cold retention policy:
//
//   - hot window: raw session directory + full index
//   - after the hot window: deterministic sampled rows are copied to a
//     per-session cold index, then the raw files and hot index are deleted
//   - after the cold window: the cold index is deleted
//   - aggregate counters are kept for 90 days
//
// Active and pinned sessions are never touched.
type Manager struct {
	cfg     config.Config
	store   *store.Store
	capture *capture.Manager
	log     *slog.Logger

	settingsMu  sync.RWMutex
	settings    Settings
	runMu       sync.Mutex
	deferredLog map[string]time.Time
}

func New(cfg config.Config, st *store.Store, mgr *capture.Manager, log *slog.Logger) *Manager {
	m := &Manager{cfg: cfg, store: st, capture: mgr, log: log, deferredLog: make(map[string]time.Time)}
	m.settings = m.loadSettings(context.Background())
	return m
}

func (m *Manager) Settings() Settings {
	m.settingsMu.RLock()
	defer m.settingsMu.RUnlock()
	return m.settings
}

func (m *Manager) loadSettings(ctx context.Context) Settings {
	s := Settings{
		HotHours:      int(m.cfg.HotRetention / time.Hour),
		ColdDays:      int(m.cfg.ColdRetention / (24 * time.Hour)),
		SamplePercent: m.cfg.SamplePercent,
	}
	load := func(key string, target *int) {
		raw, err := m.store.GetSetting(ctx, key)
		if err != nil {
			return
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			m.log.Warn("retention: invalid stored setting", "key", key, "value", raw)
			return
		}
		*target = v
	}
	load(settingHotHours, &s.HotHours)
	load(settingColdDays, &s.ColdDays)
	load(settingSamplePercent, &s.SamplePercent)
	if err := validateSettings(s); err != nil {
		m.log.Warn("retention: stored settings invalid, using environment defaults", "error", err)
		return Settings{
			HotHours:      int(m.cfg.HotRetention / time.Hour),
			ColdDays:      int(m.cfg.ColdRetention / (24 * time.Hour)),
			SamplePercent: m.cfg.SamplePercent,
		}
	}
	return s
}

// UpdateSettings validates and persists a new policy. It takes effect on the
// next hourly retention pass; already deleted data cannot be restored.
func (m *Manager) UpdateSettings(ctx context.Context, s Settings) error {
	if err := validateSettings(s); err != nil {
		return err
	}
	if err := m.store.SetSettings(ctx, map[string]string{
		settingHotHours:      strconv.Itoa(s.HotHours),
		settingColdDays:      strconv.Itoa(s.ColdDays),
		settingSamplePercent: strconv.Itoa(s.SamplePercent),
	}); err != nil {
		return err
	}
	m.settingsMu.Lock()
	m.settings = s
	m.settingsMu.Unlock()
	return nil
}

func (m *Manager) RunLoop(ctx context.Context) {
	m.runOnce(ctx)
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.runOnce(ctx)
		}
	}
}

// RunNow performs one retention pass immediately, used when disk pressure is
// detected. It still honors hot/cold windows and pinned sessions.
func (m *Manager) RunNow(ctx context.Context) {
	m.runOnce(ctx)
}

func (m *Manager) runOnce(ctx context.Context) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	settings := m.Settings()
	sessions, err := m.store.ListRetentionSessions(ctx)
	if err != nil {
		m.log.Warn("retention: list sessions", "error", err)
		return
	}
	active := m.capture.ActiveSessionID()
	now := time.Now()

	if settings.HotHours > 0 {
		samplePercent := settings.SamplePercent
		if settings.ColdDays == 0 {
			// No cold retention: do not create a cold index just to delete it.
			samplePercent = 0
		}
		for _, cs := range sessions {
			if cs.ID == active || cs.Pinned {
				continue
			}
			if cs.Status == "indexing" {
				if cs.IndexingStartedAt > 0 && now.Sub(time.UnixMilli(cs.IndexingStartedAt)) < time.Hour {
					continue
				}
				// A scan that never completed must not exempt a session from
				// retention forever.
				_ = m.store.SetCaptureSessionStatus(ctx, cs.ID, "error")
				cs.Status = "error"
			}
			if cs.Status == "expired" || cs.Status == "purged" {
				continue
			}
			if cs.Status == "error" {
				if last, ok := m.deferredLog[cs.ID]; !ok || now.Sub(last) > time.Hour {
					m.log.Warn("retention: skip session with index error", "session", cs.ID)
					m.deferredLog[cs.ID] = now
				}
				continue
			}
			if now.Sub(time.UnixMilli(cs.CaptureDate)) < settings.HotDuration() {
				continue
			}
			if samplePercent > 0 && !fileExists(m.indexPath("hot", cs.ID)) && dirExists(cs.Dir) {
				// The raw data may still need its first index pass. Deleting it now
				// would leave neither a hot nor a sampled cold index.
				if last, ok := m.deferredLog[cs.ID]; !ok || now.Sub(last) > time.Hour {
					m.log.Warn("retention: defer session without a completed index", "session", cs.ID)
					m.deferredLog[cs.ID] = now
				}
				continue
			}
			if err := m.expire(ctx, cs, samplePercent); err != nil {
				m.log.Warn("retention: expire session", "session", cs.ID, "error", err)
			}
		}
	}

	// ColdDays == 0 means "do not keep cold indexes"; purge all expired cold
	// indexes immediately instead of silently disabling the purge.
	m.purgeCold(ctx, sessions, now, settings.ColdDuration())
	// Aggregate counters are kept for 90 days independently of the raw/sampled
	// session indexes.
	_ = m.store.PurgeAggregates(ctx, now.Add(-90*24*time.Hour))
}

func (m *Manager) expire(ctx context.Context, cs store.CaptureSession, samplePercent int) error {
	if cs.ID == m.capture.ActiveSessionID() {
		return nil
	}
	root := filepath.Join(m.cfg.FluxzyDataDir, "sessions")
	if !safeUnder(cs.Dir, root) || filepath.Base(filepath.Clean(cs.Dir)) != cs.ID {
		return fmt.Errorf("unsafe session dir %q", cs.Dir)
	}

	hotPath := m.indexPath("hot", cs.ID)
	coldPath := m.indexPath("cold", cs.ID)

	// Flush/close any writer first so the hot index is stable while we copy.
	m.capture.RemoveSession(cs.ID)

	if fileExists(hotPath) && samplePercent > 0 {
		if err := m.copySampled(ctx, hotPath, coldPath, cs.ID, samplePercent); err != nil {
			return fmt.Errorf("copy sampled rows: %w", err)
		}
	}

	if err := os.RemoveAll(cs.Dir); err != nil {
		return fmt.Errorf("remove session dir: %w", err)
	}
	if err := removeIndex(hotPath); err != nil {
		return fmt.Errorf("remove hot index: %w", err)
	}
	_ = m.store.DeleteSessionIndexErrors(ctx, cs.ID)
	if err := m.store.SetCaptureSessionStatus(ctx, cs.ID, "expired"); err != nil {
		return err
	}
	delete(m.deferredLog, cs.ID)
	m.log.Info("retention: session expired", "session", cs.ID, "cold", fileExists(coldPath))
	return nil
}

func (m *Manager) copySampled(ctx context.Context, hotPath, coldPath, sessionID string, samplePercent int) error {
	src, err := store.OpenIndex(hotPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := store.OpenIndex(coldPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	maxID, err := src.MaxID(ctx)
	if err != nil {
		return err
	}
	before := maxID + 1
	for before > 1 {
		rows, _, err := src.Query(ctx, store.ExchangeFilter{Limit: 1000, BeforeID: before})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		sampled := make([]store.ExchangeRow, 0, len(rows))
		for _, r := range rows {
			if shouldSample(sessionID, r.ID, samplePercent) {
				sampled = append(sampled, r)
			}
		}
		if len(sampled) > 0 {
			if err := dst.UpsertBatch(ctx, sampled); err != nil {
				return err
			}
		}
		before = rows[len(rows)-1].ID
		if len(rows) < 1000 {
			break
		}
	}
	return nil
}

func (m *Manager) purgeCold(ctx context.Context, sessions []store.CaptureSession, now time.Time, coldRetention time.Duration) {
	for _, cs := range sessions {
		if cs.Pinned || cs.Status != "expired" {
			continue
		}
		if now.Sub(time.UnixMilli(cs.CaptureDate)) < coldRetention {
			continue
		}
		if err := removeIndex(m.indexPath("cold", cs.ID)); err != nil {
			m.log.Warn("retention: purge cold index", "session", cs.ID, "error", err)
			continue
		}
		_ = m.store.DeleteSessionIndexErrors(ctx, cs.ID)
		_ = m.store.SetCaptureSessionStatus(ctx, cs.ID, "purged")
		m.log.Info("retention: cold index purged", "session", cs.ID)
	}
}

func (m *Manager) indexPath(kind, sessionID string) string {
	return filepath.Join(m.cfg.ConsoleDir, "index", kind+"-"+store.SanitizeSessionID(sessionID)+".db")
}

func shouldSample(sessionID string, id int64, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", sessionID, id)))
	return int(h.Sum32()%100) < percent
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// removeIndex removes a SQLite database and all of its sidecars. Deleting only
// the main file can leave WAL pages containing the data we intended to purge.
func removeIndex(p string) error {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(p + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func safeUnder(path, root string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
