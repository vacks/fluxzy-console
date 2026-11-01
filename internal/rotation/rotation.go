package rotation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
)

const (
	keyEnabled        = "rotation.enabled"
	keyIntervalHours  = "rotation.interval_hours"
	keyMaxExchanges   = "rotation.max_exchanges"
	keyMaxSessionSize = "rotation.max_session_bytes"
	keyMinFreeBytes   = "rotation.min_free_bytes"
	keyEmergencyFree  = "rotation.emergency_free_bytes"
	keyCheckSeconds   = "rotation.check_seconds"
)

type Settings struct {
	Enabled            bool  `json:"enabled"`
	IntervalHours      int   `json:"intervalHours"`
	MaxExchanges       int64 `json:"maxExchanges"`
	MaxSessionBytes    int64 `json:"maxSessionBytes"`
	MinFreeBytes       int64 `json:"minFreeBytes"`
	EmergencyFreeBytes int64 `json:"emergencyFreeBytes"`
	CheckSeconds       int   `json:"checkSeconds"`
}

func Defaults() Settings {
	return Settings{
		Enabled:            true,
		IntervalHours:      0,
		MaxExchanges:       0,
		MaxSessionBytes:    0,
		MinFreeBytes:       2_000_000_000,
		EmergencyFreeBytes: 500_000_000,
		CheckSeconds:       30,
	}
}

func (s Settings) Validate() error {
	if s.IntervalHours < 0 || s.IntervalHours > 8760 {
		return fmt.Errorf("轮转间隔必须在 0–8760 小时之间")
	}
	if s.MaxExchanges < 0 {
		return fmt.Errorf("最大 exchange 数不能为负数")
	}
	if s.MaxSessionBytes < 0 {
		return fmt.Errorf("最大会话大小不能为负数")
	}
	if s.MinFreeBytes < 0 || s.EmergencyFreeBytes < 0 {
		return fmt.Errorf("磁盘阈值不能为负数")
	}
	if s.EmergencyFreeBytes > s.MinFreeBytes {
		return fmt.Errorf("紧急磁盘阈值不能大于最小剩余空间")
	}
	if s.CheckSeconds < 5 || s.CheckSeconds > 3600 {
		return fmt.Errorf("检查间隔必须在 5–3600 秒之间")
	}
	return nil
}

type DiskInfo struct {
	FreeBytes   int64   `json:"freeBytes"`
	TotalBytes  int64   `json:"totalBytes"`
	UsedPercent float64 `json:"usedPercent"`
}

type RetentionRunner interface {
	RunNow(ctx context.Context)
}

type Status struct {
	Settings        Settings `json:"settings"`
	Defaults        Settings `json:"defaults"`
	Disk            DiskInfo `json:"disk"`
	ActiveSessionID string   `json:"activeSessionId"`
	SessionBytes    int64    `json:"sessionBytes"`
	LastRotatedAt   int64    `json:"lastRotatedAt"`
	LastReason      string   `json:"lastReason"`
	NextCheckAt     int64    `json:"nextCheckAt"`
}

type Manager struct {
	store     *store.Store
	sup       *supervisor.Supervisor
	capture   *capture.Manager
	retention RetentionRunner
	dataDir   string
	hub       *live.Hub
	log       *slog.Logger

	mu               sync.RWMutex
	settings         Settings
	lastAt           int64
	lastWhy          string
	diskWindow       time.Time
	diskAttempts     int
	diskCooldownTill time.Time
	diskWarnAt       time.Time

	opMu sync.Mutex

	sizeMu  sync.Mutex
	sizeID  string
	sizeVal int64
	sizeAt  time.Time
}

func New(st *store.Store, sup *supervisor.Supervisor, cap *capture.Manager, retention RetentionRunner, dataDir string, hub *live.Hub, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{
		store: st, sup: sup, capture: cap, retention: retention,
		dataDir: dataDir, hub: hub, log: log, settings: Defaults(),
	}
	m.settings = m.loadSettings(context.Background())
	return m
}

func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Manager) loadSettings(ctx context.Context) Settings {
	s := Defaults()
	loadInt := func(key string, target *int) {
		raw, err := m.store.GetSetting(ctx, key)
		if err != nil {
			return
		}
		v, err := strconv.Atoi(raw)
		if err == nil {
			*target = v
		}
	}
	loadInt64 := func(key string, target *int64) {
		raw, err := m.store.GetSetting(ctx, key)
		if err != nil {
			return
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			*target = v
		}
	}
	if raw, err := m.store.GetSetting(ctx, keyEnabled); err == nil {
		s.Enabled = strings.EqualFold(raw, "true") || raw == "1"
	}
	loadInt(keyIntervalHours, &s.IntervalHours)
	loadInt64(keyMaxExchanges, &s.MaxExchanges)
	loadInt64(keyMaxSessionSize, &s.MaxSessionBytes)
	loadInt64(keyMinFreeBytes, &s.MinFreeBytes)
	loadInt64(keyEmergencyFree, &s.EmergencyFreeBytes)
	loadInt(keyCheckSeconds, &s.CheckSeconds)
	if err := s.Validate(); err != nil {
		m.log.Warn("rotation: invalid stored settings, using defaults", "error", err)
		return Defaults()
	}
	return s
}

func (m *Manager) UpdateSettings(ctx context.Context, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := m.store.SetSettings(ctx, map[string]string{
		keyEnabled:        strconv.FormatBool(s.Enabled),
		keyIntervalHours:  strconv.Itoa(s.IntervalHours),
		keyMaxExchanges:   strconv.FormatInt(s.MaxExchanges, 10),
		keyMaxSessionSize: strconv.FormatInt(s.MaxSessionBytes, 10),
		keyMinFreeBytes:   strconv.FormatInt(s.MinFreeBytes, 10),
		keyEmergencyFree:  strconv.FormatInt(s.EmergencyFreeBytes, 10),
		keyCheckSeconds:   strconv.Itoa(s.CheckSeconds),
	}); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}

func (m *Manager) RunLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Since(last) < time.Duration(m.Settings().CheckSeconds)*time.Second {
				continue
			}
			last = time.Now()
			m.RunOnce(ctx)
		}
	}
}

func (m *Manager) RunOnce(ctx context.Context) {
	if !m.opMu.TryLock() {
		return
	}
	defer m.opMu.Unlock()

	s := m.Settings()
	if m.sup == nil {
		return
	}
	st := m.sup.Status()
	disk, diskErr := DiskInfoOf(m.dataDir)
	if diskErr != nil {
		m.log.Warn("rotation: cannot read disk usage; skipping disk triggers", "error", diskErr)
	}
	diskKnown := diskErr == nil
	if diskKnown && disk.FreeBytes >= s.MinFreeBytes {
		m.resetDiskRotation()
	}

	// A disk-full stop is recoverable: once enough space is free, start a new
	// session instead of leaving fluxzy down until an operator notices.
	if st.State == supervisor.StateDiskFull {
		if s.Enabled && diskKnown && disk.FreeBytes >= s.MinFreeBytes {
			if err := m.sup.Start(ctx); err != nil {
				m.log.Warn("rotation: restart after disk recovery failed", "error", err)
				return
			}
			m.record("disk_recovered")
			m.publish("disk_recovered", m.capture.ActiveSessionID())
		}
		return
	}
	if !s.Enabled || st.State != supervisor.StateRunning {
		return
	}
	activeID := m.capture.ActiveSessionID()
	if activeID == "" {
		return
	}
	cs, err := m.store.GetCaptureSession(ctx, activeID)
	if err != nil {
		return
	}

	if diskKnown && disk.FreeBytes < s.EmergencyFreeBytes {
		m.log.Warn("rotation: disk emergency", "free", disk.FreeBytes)
		_ = m.sup.Stop(ctx, "disk_full")
		m.sup.MarkDiskFull()
		m.record("disk_full")
		m.publish("disk_full", activeID)
		return
	}
	if diskKnown && disk.FreeBytes < s.MinFreeBytes {
		if m.retention != nil {
			m.retention.RunNow(ctx)
		}
		disk, diskErr = DiskInfoOf(m.dataDir)
		if diskErr != nil {
			return
		}
		if disk.FreeBytes < s.MinFreeBytes {
			if !m.allowDiskRotation(time.Now()) {
				return
			}
			m.rotate(ctx, "disk_low", activeID, cs)
			return
		}
	}

	started := cs.CaptureDate
	if started == 0 {
		started = st.StartedAt
	}
	if s.IntervalHours > 0 && started > 0 {
		if time.Since(time.UnixMilli(started)) >= time.Duration(s.IntervalHours)*time.Hour {
			m.rotate(ctx, "interval", activeID, cs)
			return
		}
	}
	total := m.capture.Status().Stats.Total
	if s.MaxExchanges > 0 && total >= s.MaxExchanges {
		m.rotate(ctx, "exchanges", activeID, cs)
		return
	}
	if s.MaxSessionBytes > 0 {
		size := cs.TotalBytes
		if dirSize, ok := m.sessionSize(ctx, activeID, cs.Dir); ok && dirSize > size {
			size = dirSize
		}
		if size >= s.MaxSessionBytes {
			m.rotate(ctx, "bytes", activeID, cs)
			return
		}
	}
}

func (m *Manager) rotate(ctx context.Context, reason, activeID string, cs *store.CaptureSession) {
	m.log.Info("rotation: restarting fluxzy", "reason", reason, "session", activeID)
	opCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := m.sup.Rotate(opCtx, reason); err != nil {
		if errors.Is(err, supervisor.ErrNotRunning) {
			return
		}
		m.log.Warn("rotation: restart failed", "reason", reason, "error", err)
		return
	}
	m.record(reason)
	m.publish(reason, m.capture.ActiveSessionID())
}

func (m *Manager) allowDiskRotation(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.Before(m.diskCooldownTill) {
		if now.Sub(m.diskWarnAt) >= 10*time.Minute {
			m.log.Warn("rotation: disk still low; waiting for cooldown")
			m.diskWarnAt = now
		}
		return false
	}
	if now.Sub(m.diskWindow) >= time.Hour {
		m.diskWindow = now
		m.diskAttempts = 0
	}
	if m.diskAttempts >= 3 {
		m.diskCooldownTill = now.Add(time.Hour)
		m.diskWarnAt = now
		m.log.Warn("rotation: disk still low after repeated rotations; pausing disk rotation for an hour")
		return false
	}
	m.diskAttempts++
	m.diskCooldownTill = now.Add(10 * time.Minute)
	return true
}

func (m *Manager) resetDiskRotation() {
	m.mu.Lock()
	m.diskAttempts = 0
	m.diskWindow = time.Time{}
	m.diskCooldownTill = time.Time{}
	m.mu.Unlock()
}

func (m *Manager) record(reason string) {
	m.mu.Lock()
	m.lastAt = time.Now().UnixMilli()
	m.lastWhy = reason
	m.mu.Unlock()
}

func (m *Manager) publish(reason, sessionID string) {
	if m.hub != nil {
		m.hub.Publish(live.Event{Type: "rotation", Data: map[string]any{
			"reason": reason, "sessionId": sessionID, "at": time.Now().UnixMilli(),
		}})
	}
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	s, lastAt, lastWhy := m.settings, m.lastAt, m.lastWhy
	m.mu.RUnlock()
	activeID := ""
	if m.capture != nil {
		activeID = m.capture.ActiveSessionID()
	}
	var size int64
	if activeID != "" {
		if cs, err := m.store.GetCaptureSession(context.Background(), activeID); err == nil {
			size = cs.TotalBytes
		}
	}
	disk, _ := DiskInfoOf(m.dataDir)
	return Status{
		Settings:        s,
		Defaults:        Defaults(),
		Disk:            disk,
		ActiveSessionID: activeID,
		SessionBytes:    size,
		LastRotatedAt:   lastAt,
		LastReason:      lastWhy,
		NextCheckAt:     time.Now().Add(time.Duration(s.CheckSeconds) * time.Second).UnixMilli(),
	}
}

func (m *Manager) sessionSize(ctx context.Context, id, dir string) (int64, bool) {
	m.sizeMu.Lock()
	if m.sizeID == id && time.Since(m.sizeAt) < time.Minute {
		v := m.sizeVal
		m.sizeMu.Unlock()
		return v, true
	}
	m.sizeMu.Unlock()

	walkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-walkCtx.Done():
			return walkCtx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return total, false
	}
	m.sizeMu.Lock()
	m.sizeID, m.sizeVal, m.sizeAt = id, total, time.Now()
	m.sizeMu.Unlock()
	return total, true
}

func DiskInfoOf(path string) (DiskInfo, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskInfo{}, err
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize
	used := 0.0
	if total > 0 {
		used = float64(total-free) / float64(total) * 100
	}
	return DiskInfo{FreeBytes: free, TotalBytes: total, UsedPercent: used}, nil
}
