package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/sessiondir"
	"github.com/vacks/fluxzy-console/internal/store"
)

type Status struct {
	Online          bool                  `json:"online"`
	ActiveSessionID string                `json:"activeSessionId"`
	Stats           Stats                 `json:"stats"`
	Indexing        bool                  `json:"indexing"`
	ActiveSession   *store.CaptureSession `json:"-"`
}

type fileTask struct {
	sessionID string
	path      string
}

type Manager struct {
	cfg   config.Config
	store *store.Store
	hub   *live.Hub
	agg   *Aggregator
	log   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu             sync.RWMutex
	sessions       map[string]*Session
	activeID       string
	online         atomic.Bool
	processRunning atomic.Bool

	expectedMu  sync.RWMutex
	expectedID  string
	expectedDir string
	tombstones  map[string]struct{}

	statusMu     sync.RWMutex
	cachedStatus Status
	statusSet    bool

	proxyMu   sync.RWMutex
	proxyAddr string

	rootWatcher   *fsnotify.Watcher
	activeWatcher *fsnotify.Watcher
	watcherMu     sync.Mutex

	debounceMu sync.Mutex
	debounce   map[string]*time.Timer
	fileQueue  chan fileTask
	rescan     atomic.Bool
	rescanFrom atomic.Int64
	indexing   atomic.Bool

	// knownDirs contains every session directory whose meta.json was read at
	// least once. The periodic catch-up loop only retries unknown directories,
	// avoiding a database query for every archived session every 5 seconds.
	known map[string]struct{}

	// discoverMu serializes session discovery triggered by the root watcher and
	// by the periodic catch-up loop.
	discoverMu sync.Mutex
	openMu     sync.Mutex

	// Archived sessions are indexed by a small bounded worker pool. Opening a
	// Writer/SQLite pool for every historical session at startup exhausted
	// goroutines and file descriptors after a few hundred sessions.
	scanQueue    chan string
	scanQueued   map[string]struct{}
	scanMu       sync.Mutex
	scanCancelMu sync.Mutex
	scanCancels  map[string]context.CancelFunc

	discoveryRetryMu sync.Mutex
	discoveryRetry   map[string]time.Time
}

func NewManager(cfg config.Config, st *store.Store, hub *live.Hub, agg *Aggregator, log *slog.Logger) *Manager {
	m := &Manager{
		cfg:            cfg,
		store:          st,
		hub:            hub,
		agg:            agg,
		log:            log,
		sessions:       make(map[string]*Session),
		known:          make(map[string]struct{}),
		tombstones:     make(map[string]struct{}),
		proxyAddr:      cfg.FluxzyProxyAddr,
		debounce:       make(map[string]*time.Timer),
		fileQueue:      make(chan fileTask, 8192),
		scanQueue:      make(chan string, 8192),
		scanQueued:     make(map[string]struct{}),
		scanCancels:    make(map[string]context.CancelFunc),
		discoveryRetry: make(map[string]time.Time),
	}
	m.rescanFrom.Store(math.MaxInt64)
	return m
}

func (m *Manager) Start(parent context.Context) error {
	m.ctx, m.cancel = context.WithCancel(parent)

	// Start the bounded scan workers before loadExisting queues the backlog;
	// otherwise a large history fills scanQueue before any worker exists.
	for i := 0; i < 2; i++ {
		m.wg.Add(1)
		go m.scanWorker()
	}
	if err := m.loadExisting(); err != nil {
		m.log.Warn("load existing sessions", "error", err)
	}

	for i := 0; i < 4; i++ {
		m.wg.Add(1)
		go m.worker()
	}

	m.wg.Add(1)
	go m.onlineLoop()

	m.wg.Add(1)
	go m.rescanLoop()

	m.wg.Add(1)
	go m.discoveryLoop()

	m.wg.Add(1)
	go m.archiveSweepLoop()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.agg.FlushLoop(m.ctx, 30*time.Second)
	}()

	m.refreshStatus()
	m.wg.Add(1)
	go m.statusLoop()

	if err := m.startRootWatcher(); err != nil {
		m.log.Warn("watch sessions root", "error", err)
	}

	m.mu.RLock()
	active := m.activeID
	m.mu.RUnlock()
	if active != "" {
		m.switchActive(active)
	}
	return nil
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.watcherMu.Lock()
	if m.rootWatcher != nil {
		_ = m.rootWatcher.Close()
	}
	if m.activeWatcher != nil {
		_ = m.activeWatcher.Close()
	}
	m.watcherMu.Unlock()

	m.wg.Wait()

	m.mu.Lock()
	for _, s := range m.sessions {
		s.Close()
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
}

func (m *Manager) Online() bool { return m.online.Load() }

func (m *Manager) SetProcessRunning(running bool) {
	m.processRunning.Store(running)
	if !running {
		m.online.Store(false)
	}
	m.refreshStatus()
}

func (m *Manager) SetProxyAddr(addr string) {
	if addr == "" {
		return
	}
	m.proxyMu.Lock()
	m.proxyAddr = addr
	m.proxyMu.Unlock()
}

func (m *Manager) ActiveSessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeID
}

// SetExpectedSession tells the manager which session the supervisor is about
// to start. The manager keeps it pending until meta.json is readable.
func (m *Manager) SetExpectedSession(id, dir string) {
	m.expectedMu.Lock()
	m.expectedID = id
	m.expectedDir = dir
	m.expectedMu.Unlock()
}

func (m *Manager) ExpectedSession() (string, string) {
	m.expectedMu.RLock()
	defer m.expectedMu.RUnlock()
	return m.expectedID, m.expectedDir
}

// WaitForExpectedActive waits until fluxzy's new session has been discovered
// and switched to active. Startup requests use this instead of assuming the
// fsnotify switch is instant.
func (m *Manager) WaitForExpectedActive(ctx context.Context, timeout time.Duration) string {
	expected, _ := m.ExpectedSession()
	if expected == "" {
		return m.ActiveSessionID()
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if id := m.ActiveSessionID(); id == expected {
			return id
		}
		select {
		case <-ctx.Done():
			return ""
		case <-deadline.C:
			return m.ActiveSessionID()
		case <-ticker.C:
		}
	}
}

// EndActiveSession clears the active marker after fluxzy has stopped. The
// session writer is left open until retention or deletion so a running full
// scan can complete; it is idle because no new files are being produced.
func (m *Manager) EndActiveSession(id, reason string) {
	m.mu.Lock()
	if m.activeID == id {
		m.activeID = ""
	}
	delete(m.tombstones, id)
	m.mu.Unlock()
	m.expectedMu.Lock()
	if m.expectedID == id {
		m.expectedID = ""
		m.expectedDir = ""
	}
	m.expectedMu.Unlock()
	status := "stopped"
	m.mu.RLock()
	if s := m.sessions[id]; s != nil && s.ScanComplete() {
		status = "indexed"
	}
	m.mu.RUnlock()
	// The capture context is cancelled during shutdown. Persist the final
	// session marker with a bounded, non-cancelled context instead.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	defer cancel()
	if err := m.store.MarkSessionEnded(ctx, id, status, reason); err != nil {
		m.log.Warn("mark session ended", "session", id, "error", err)
	}
	m.refreshStatus()
}

func (m *Manager) isTombstoned(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tombstones[id]
	return ok
}

func (m *Manager) Status() Status {
	m.statusMu.RLock()
	st, ok := m.cachedStatus, m.statusSet
	m.statusMu.RUnlock()
	if ok {
		return st
	}
	return m.snapshotStatus()
}

func (m *Manager) snapshotStatus() Status {
	id := m.ActiveSessionID()
	stats := m.agg.Snapshot(id)
	var active *store.CaptureSession
	if id != "" {
		m.mu.RLock()
		s := m.sessions[id]
		m.mu.RUnlock()
		if s != nil {
			// Build the active-session summary from memory. Status is polled
			// every second and must not issue a SQLite read each time.
			active = &store.CaptureSession{
				ID:            s.ID,
				Dir:           s.Dir,
				CaptureDate:   s.Meta.CaptureDate.UnixMilli(),
				FluxzyVersion: s.Meta.FluxzyVersion,
				Status:        "live",
				ExchangeCount: stats.Total,
				TotalBytes:    stats.ReqBytes + stats.RespBytes,
				LastIndexedAt: time.Now().UnixMilli(),
			}
			if s.Meta.CaptureDate.IsZero() {
				active.CaptureDate = 0
			}
		}
	}
	return Status{
		Online:          m.online.Load(),
		ActiveSessionID: id,
		Stats:           stats,
		Indexing:        m.indexing.Load(),
		ActiveSession:   active,
	}
}

func (m *Manager) refreshStatus() {
	st := m.snapshotStatus()
	m.statusMu.Lock()
	m.cachedStatus = st
	m.statusSet = true
	m.statusMu.Unlock()
}

func (m *Manager) statusLoop() {
	defer m.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.refreshStatus()
		}
	}
}

func (m *Manager) archiveSweepLoop() {
	defer m.wg.Done()
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			for _, id := range m.idleArchived(10 * time.Minute) {
				m.closeIfIdle(id, 10*time.Minute)
			}
		}
	}
}

func (m *Manager) idleArchived(idleFor time.Duration) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0)
	for id, s := range m.sessions {
		if id == m.activeID {
			continue
		}
		if !s.Scanning() && s.IdleSince() >= idleFor {
			out = append(out, id)
		}
	}
	return out
}

func (m *Manager) RemoveSession(id string) {
	m.cancelScan(id)
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.tombstones[id] = struct{}{}
	if m.activeID == id {
		m.activeID = ""
	}
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}
	m.agg.Remove(id)
	m.refreshStatus()
}

// AcquireSession pins a cached session for the duration of a request. The
// returned release function must be called; idle sweeps will not close a
// session with outstanding leases.
func (m *Manager) AcquireSession(id string) (*Session, func(), bool) {
	for attempt := 0; attempt < 3; attempt++ {
		s, ok := m.GetSession(id)
		if !ok {
			return nil, nil, false
		}
		m.mu.RLock()
		current := m.sessions[id]
		if current == s {
			s.refs.Add(1)
			m.mu.RUnlock()
			return s, func() { s.refs.Add(-1) }, true
		}
		m.mu.RUnlock()
	}
	return nil, nil, false
}

func (m *Manager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	s := m.sessions[id]
	active := m.activeID
	m.mu.RUnlock()
	if s != nil {
		s.Touch()
		return s, true
	}
	var err error
	if id == active {
		s, err = m.openSession(id)
	} else {
		s, err = m.openArchived(id)
	}
	if err != nil {
		return nil, false
	}
	s.Touch()
	return s, true
}

func (m *Manager) loadExisting() error {
	root := filepath.Join(m.cfg.FluxzyDataDir, "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if !e.IsDir() || m.isTombstoned(e.Name()) {
			continue
		}
		id := e.Name()
		if !sessiondir.ValidID(id) {
			m.log.Warn("ignore session directory with invalid id", "session", id)
			continue
		}
		dir := filepath.Join(root, id)
		meta, raw, err := readMetaRaw(dir)
		if err != nil {
			// A failed start can leave an empty directory behind. It is safe to
			// remove it while fluxzy is not running; non-empty partial dirs are
			// left for discovery retries.
			if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) == 0 {
				_ = os.Remove(dir)
			}
			continue
		}
		m.persistSession(id, dir, meta, raw)
	}

	// No session is active until the supervisor creates one. Sessions that
	// were left with a live marker by a previous process are archived now.
	if err := m.store.DemoteLiveSessions(m.ctx, ""); err != nil {
		m.log.Warn("demote stale live sessions", "error", err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	toScan := make([]string, 0, len(ids))
	for _, id := range ids {
		cs, _ := m.store.GetCaptureSession(m.ctx, id)
		hotIndex := filepath.Join(m.cfg.ConsoleDir, "index", "hot-"+store.SanitizeSessionID(id)+".db")
		if cs != nil && cs.Status == "indexed" && fileExists(hotIndex) {
			continue
		}
		toScan = append(toScan, id)
	}
	if len(toScan) > 0 {
		// Mark the backlog as indexing before retention can decide that a
		// session without a hot index is old enough to delete.
		for _, id := range toScan {
			if err := m.store.MarkSessionIndexing(m.ctx, id); err != nil {
				m.log.Warn("mark session indexing", "session", id, "error", err)
			}
		}
		// Do not open archived sessions here. A bounded worker opens them one
		// at a time, scans, and closes them again if they are not active. Queue
		// in the background so startup is not delayed by a large history.
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for _, id := range toScan {
				m.queueScan(id)
			}
		}()
	}
	return nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func (m *Manager) persistSession(id, dir string, meta Meta, raw string) {
	status := "indexing"
	rulesHash, configHash, rotationReason := "", "", ""
	var indexingStartedAt, indexedAt int64
	existing, err := m.store.GetCaptureSession(m.ctx, id)
	if err == nil {
		status = existing.Status
		rulesHash = existing.RulesHash
		configHash = existing.ConfigHash
		rotationReason = existing.RotationReason
		indexingStartedAt = existing.IndexingStartedAt
		indexedAt = existing.IndexedAt
	}
	_ = m.store.UpsertCaptureSession(m.ctx, store.CaptureSession{
		ID:                id,
		Dir:               dir,
		CaptureDate:       meta.CaptureDate.UnixMilli(),
		FluxzyVersion:     meta.FluxzyVersion,
		Status:            status,
		MetaJSON:          raw,
		RulesHash:         rulesHash,
		ConfigHash:        configHash,
		RotationReason:    rotationReason,
		IndexingStartedAt: indexingStartedAt,
		IndexedAt:         indexedAt,
	})
	m.mu.Lock()
	m.known[id] = struct{}{}
	m.mu.Unlock()
}

func (m *Manager) openSession(id string) (*Session, error) {
	if m.isTombstoned(id) {
		return nil, fmt.Errorf("session %s is being removed", id)
	}
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s != nil && s.Writer != nil {
		s.Touch()
		return s, nil
	}

	m.openMu.Lock()
	defer m.openMu.Unlock()

	m.mu.RLock()
	s = m.sessions[id]
	m.mu.RUnlock()
	if s != nil && s.Writer != nil {
		s.Touch()
		return s, nil
	}
	if s != nil {
		m.mu.Lock()
		if current := m.sessions[id]; current == s {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
		s.Close()
	}
	if m.isTombstoned(id) {
		return nil, fmt.Errorf("session %s is being removed", id)
	}

	cs, err := m.store.GetCaptureSession(m.ctx, id)
	if err != nil {
		return nil, err
	}
	meta, raw, err := readMetaRaw(cs.Dir)
	if err != nil {
		return nil, err
	}
	if cs.MetaJSON == "" {
		_ = m.store.UpsertCaptureSession(m.ctx, store.CaptureSession{
			ID:            id,
			Dir:           cs.Dir,
			CaptureDate:   meta.CaptureDate.UnixMilli(),
			FluxzyVersion: meta.FluxzyVersion,
			Status:        cs.Status,
			MetaJSON:      raw,
		})
	}

	indexPath := filepath.Join(m.cfg.ConsoleDir, "index", "hot-"+store.SanitizeSessionID(id)+".db")
	idx, err := store.OpenIndex(indexPath)
	if err != nil {
		return nil, err
	}
	s = OpenSession(m.ctx, id, cs.Dir, meta, idx, m.hub, m.agg, m.store, m.log)
	s.Touch()

	m.mu.Lock()
	if current := m.sessions[id]; current != nil && current.Writer != nil {
		m.mu.Unlock()
		s.Close()
		return current, nil
	}
	if _, removed := m.tombstones[id]; removed {
		m.mu.Unlock()
		s.Close()
		return nil, fmt.Errorf("session %s is being removed", id)
	}
	m.sessions[id] = s
	m.evictSessionsLocked()
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) evictSessionsLocked() {
	const maxCached = 128
	if len(m.sessions) <= maxCached {
		return
	}
	var oldestID string
	var oldest time.Duration
	for id, s := range m.sessions {
		if id == m.activeID || s.Scanning() || s.refs.Load() > 0 {
			continue
		}
		idle := s.IdleSince()
		if oldestID == "" || idle > oldest {
			oldestID, oldest = id, idle
		}
	}
	if oldestID == "" {
		return
	}
	s := m.sessions[oldestID]
	delete(m.sessions, oldestID)
	go func() {
		s.Close()
		m.agg.Remove(oldestID)
	}()
}

func (m *Manager) openArchived(id string) (*Session, error) {
	if m.isTombstoned(id) {
		return nil, fmt.Errorf("session %s is being removed", id)
	}
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s != nil {
		s.Touch()
		return s, nil
	}

	m.openMu.Lock()
	defer m.openMu.Unlock()
	m.mu.RLock()
	s = m.sessions[id]
	m.mu.RUnlock()
	if s != nil {
		s.Touch()
		return s, nil
	}
	if m.isTombstoned(id) {
		return nil, fmt.Errorf("session %s is being removed", id)
	}
	cs, err := m.store.GetCaptureSession(m.ctx, id)
	if err != nil {
		return nil, err
	}
	meta, _, err := readMetaRaw(cs.Dir)
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(m.cfg.ConsoleDir, "index", "hot-"+store.SanitizeSessionID(id)+".db")
	idx, err := store.OpenIndex(indexPath)
	if err != nil {
		return nil, err
	}
	s = OpenArchivedSession(id, cs.Dir, meta, idx, m.log)
	s.Touch()

	m.mu.Lock()
	if current := m.sessions[id]; current != nil {
		m.mu.Unlock()
		s.Close()
		return current, nil
	}
	if _, removed := m.tombstones[id]; removed {
		m.mu.Unlock()
		s.Close()
		return nil, fmt.Errorf("session %s is being removed", id)
	}
	m.sessions[id] = s
	m.evictSessionsLocked()
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) startScan(s *Session, active bool) {
	if s.indexed.Load() {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.scanSession(s, active)
	}()
}

func (m *Manager) scanSession(s *Session, active bool) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("full scan panic", "session", s.ID, "panic", r)
			s.indexed.Store(false)
			s.scanComplete.Store(false)
			ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
			_ = m.store.SetCaptureSessionIndexError(ctx, s.ID, fmt.Sprintf("索引 panic: %v", r))
			cancel()
		}
	}()
	if s.indexed.Load() {
		return
	}
	s.scanning.Add(1)
	defer s.scanning.Add(-1)
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()
	m.scanCancelMu.Lock()
	m.scanCancels[s.ID] = cancel
	m.scanCancelMu.Unlock()
	defer func() {
		m.scanCancelMu.Lock()
		delete(m.scanCancels, s.ID)
		m.scanCancelMu.Unlock()
	}()

	_ = m.store.MarkSessionIndexing(ctx, s.ID)
	if active {
		m.indexing.Store(true)
		defer m.indexing.Store(false)
	}
	if err := s.FullScan(ctx, active); err != nil {
		if m.isTombstoned(s.ID) || m.ctx.Err() != nil {
			return
		}
		if errors.Is(err, errExchangesNotReady) {
			m.log.Info("session exchanges not ready; retrying scan", "session", s.ID)
			time.AfterFunc(2*time.Second, func() {
				if m.ctx.Err() == nil {
					m.queueScan(s.ID)
				}
			})
			return
		}
		m.log.Warn("full scan", "session", s.ID, "error", err)
		_ = m.store.SetCaptureSessionIndexError(ctx, s.ID, err.Error())
		return
	}
	if m.isTombstoned(s.ID) {
		return
	}
	status := "indexed"
	m.mu.RLock()
	stillActive := m.activeID == s.ID
	m.mu.RUnlock()
	if stillActive {
		status = "live"
	}
	_ = m.store.SetCaptureSessionStatus(ctx, s.ID, status)
	m.log.Info("session indexed", "session", s.ID, "active", active)
}

func (m *Manager) cancelScan(id string) {
	m.scanCancelMu.Lock()
	cancel := m.scanCancels[id]
	m.scanCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) queueScan(id string) {
	m.scanMu.Lock()
	if _, queued := m.scanQueued[id]; queued {
		m.scanMu.Unlock()
		return
	}
	m.scanQueued[id] = struct{}{}
	m.scanMu.Unlock()
	select {
	case m.scanQueue <- id:
	case <-m.ctx.Done():
		m.scanMu.Lock()
		delete(m.scanQueued, id)
		m.scanMu.Unlock()
	}
}

func (m *Manager) scanWorker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case id := <-m.scanQueue:
			m.scanMu.Lock()
			delete(m.scanQueued, id)
			m.scanMu.Unlock()
			s, err := m.openSession(id)
			if err != nil {
				m.log.Warn("open session for scan", "session", id, "error", err)
				continue
			}
			m.scanSession(s, false)
			// Release the Writer/SQLite pool unless the session became active
			// or was read recently by an HTTP request.
			m.closeIfIdle(id, 2*time.Minute)
		}
	}
}

func (m *Manager) closeIfIdle(id string, idleFor time.Duration) {
	m.mu.Lock()
	if m.activeID == id {
		m.mu.Unlock()
		return
	}
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	if s.Scanning() || s.refs.Load() > 0 || s.IdleSince() < idleFor {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, id)
	m.mu.Unlock()
	s.Close()
	m.agg.Remove(id)
}

func (m *Manager) startRootWatcher() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	m.rootWatcher = w
	root := filepath.Join(m.cfg.FluxzyDataDir, "sessions")
	if err := os.MkdirAll(root, 0o750); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Add(root); err != nil {
		_ = w.Close()
		return err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-m.ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&fsnotify.Create != 0 {
					info, err := os.Stat(ev.Name)
					if err == nil && info.IsDir() {
						id := filepath.Base(ev.Name)
						name := ev.Name
						m.wg.Add(1)
						go func() {
							defer m.wg.Done()
							m.discoverNewSession(id, name)
						}()
					}
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				m.log.Warn("sessions watcher", "error", err)
				m.rescan.Store(true)
			}
		}
	}()
	return nil
}

func (m *Manager) discoverNewSession(id, dir string) {
	if !sessiondir.ValidID(id) {
		return
	}
	expectedID, _ := m.ExpectedSession()
	isExpected := expectedID == id
	if !isExpected && !m.shouldRetryDiscovery(id) {
		return
	}
	meta, raw, err := m.readMetaWithRetry(dir)
	if err != nil {
		if isExpected {
			m.clearDiscoveryFailure(id)
		} else {
			m.markDiscoveryFailure(id)
		}
		m.log.Warn("new session has no readable meta", "session", id, "error", err)
		return
	}

	m.discoverMu.Lock()
	defer m.discoverMu.Unlock()
	m.mu.RLock()
	_, opened := m.sessions[id]
	_, known := m.known[id]
	m.mu.RUnlock()
	if opened || known {
		m.clearDiscoveryFailure(id)
		return
	}
	if m.isTombstoned(id) {
		return
	}
	m.persistSession(id, dir, meta, raw)
	s, err := m.openSession(id)
	if err != nil {
		m.mu.Lock()
		delete(m.known, id)
		m.mu.Unlock()
		m.markDiscoveryFailure(id)
		m.log.Warn("open new session", "session", id, "error", err)
		return
	}
	m.clearDiscoveryFailure(id)

	m.mu.RLock()
	current := m.sessions[m.activeID]
	m.mu.RUnlock()
	expectedID, _ = m.ExpectedSession()
	shouldSwitch := current == nil || meta.CaptureDate.After(current.Meta.CaptureDate)
	if expectedID != "" {
		shouldSwitch = id == expectedID
	}

	m.startScan(s, shouldSwitch)
	if shouldSwitch {
		m.switchActive(id)
	}
}

// discoverArchived registers a historical session without opening a Writer.
// The bounded scan worker indexes it later.
func (m *Manager) discoverArchived(id, dir string) {
	if !sessiondir.ValidID(id) || !m.shouldRetryDiscovery(id) {
		return
	}
	meta, raw, err := m.readMetaWithRetry(dir)
	if err != nil {
		m.markDiscoveryFailure(id)
		return
	}
	m.discoverMu.Lock()
	m.mu.RLock()
	_, opened := m.sessions[id]
	_, known := m.known[id]
	m.mu.RUnlock()
	if opened || known {
		m.discoverMu.Unlock()
		m.clearDiscoveryFailure(id)
		return
	}
	if m.isTombstoned(id) {
		m.discoverMu.Unlock()
		return
	}
	m.persistSession(id, dir, meta, raw)
	m.clearDiscoveryFailure(id)
	m.discoverMu.Unlock()
	m.queueScan(id)
}

func (m *Manager) readMetaWithRetry(dir string) (Meta, string, error) {
	var meta Meta
	var raw string
	var err error
	for i := 0; i < 5; i++ {
		meta, raw, err = readMetaRaw(dir)
		if err == nil {
			return meta, raw, nil
		}
		select {
		case <-m.ctx.Done():
			return Meta{}, "", m.ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return Meta{}, "", err
}

func (m *Manager) shouldRetryDiscovery(id string) bool {
	m.discoveryRetryMu.Lock()
	defer m.discoveryRetryMu.Unlock()
	next, ok := m.discoveryRetry[id]
	return !ok || !time.Now().Before(next)
}

func (m *Manager) markDiscoveryFailure(id string) {
	m.discoveryRetryMu.Lock()
	m.discoveryRetry[id] = time.Now().Add(30 * time.Second)
	m.discoveryRetryMu.Unlock()
}

func (m *Manager) clearDiscoveryFailure(id string) {
	m.discoveryRetryMu.Lock()
	delete(m.discoveryRetry, id)
	m.discoveryRetryMu.Unlock()
}

func (m *Manager) switchActive(id string) {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s == nil || s.Writer == nil {
		// GetSession may have opened this session through the read-only
		// archived path just before it became active. Replace it with a
		// writable session so live indexing is not silently disabled.
		if s != nil {
			m.mu.Lock()
			if current := m.sessions[id]; current == s {
				delete(m.sessions, id)
			}
			m.mu.Unlock()
			s.Close()
		}
		var err error
		s, err = m.openSession(id)
		if err != nil {
			m.log.Warn("switch active", "session", id, "error", err)
			return
		}
	}

	m.mu.Lock()
	m.activeID = id
	m.mu.Unlock()

	if err := m.store.DemoteLiveSessions(m.ctx, id); err != nil {
		m.log.Warn("demote inactive sessions", "active", id, "error", err)
	}

	m.watcherMu.Lock()
	if m.activeWatcher != nil {
		_ = m.activeWatcher.Close()
		m.activeWatcher = nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		m.watcherMu.Unlock()
		m.log.Warn("create session watcher", "error", err)
		return
	}
	m.activeWatcher = w
	m.watcherMu.Unlock()

	if err := w.Add(s.Dir); err != nil {
		m.log.Warn("watch session directory", "session", id, "error", err)
		m.rescan.Store(true)
	}
	exchanges := filepath.Join(s.Dir, "exchanges")
	if err := m.addDirs(w, exchanges); err != nil {
		if !os.IsNotExist(err) {
			m.log.Warn("watch session exchanges", "session", id, "error", err)
		}
		m.rescan.Store(true)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-m.ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				m.handleWatchEvent(id, w, ev)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				m.log.Warn("session watcher", "session", id, "error", err)
				m.rescan.Store(true)
			}
		}
	}()

	m.refreshStatus()
	m.hub.Publish(live.Event{Type: "session", Data: map[string]any{
		"activeSessionId": id,
		"online":          m.online.Load(),
	}})
}

func (m *Manager) handleWatchEvent(sessionID string, w *fsnotify.Watcher, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create != 0 {
		info, err := os.Stat(ev.Name)
		if err == nil && info.IsDir() {
			if err := m.addDirs(w, ev.Name); err != nil {
				m.rescan.Store(true)
				m.log.Warn("watch session subdirectory", "path", ev.Name, "error", err)
			}
			m.scheduleTree(sessionID, ev.Name)
			return
		}
	}
	if strings.HasSuffix(ev.Name, ".mpack") && (ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0) {
		m.scheduleFile(sessionID, ev.Name)
	}
}

func (m *Manager) addDirs(w *fsnotify.Watcher, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	var firstErr error
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if addErr := w.Add(path); addErr != nil && firstErr == nil {
				firstErr = addErr
			}
		}
		return nil
	})
	if firstErr != nil {
		return firstErr
	}
	return err
}

func (m *Manager) scheduleTree(sessionID, root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mpack") {
			m.scheduleFile(sessionID, path)
		}
		return nil
	})
}

func (m *Manager) scheduleFile(sessionID, path string) {
	key := sessionID + "\x00" + path
	m.debounceMu.Lock()
	if t, ok := m.debounce[key]; ok {
		t.Stop()
	}
	m.debounce[key] = time.AfterFunc(70*time.Millisecond, func() {
		m.debounceMu.Lock()
		delete(m.debounce, key)
		m.debounceMu.Unlock()
		if !m.enqueueFile(fileTask{sessionID: sessionID, path: path}) {
			m.noteRescan(filepath.Base(filepath.Dir(path)))
		}
	})
	m.debounceMu.Unlock()
}

func (m *Manager) enqueueFile(task fileTask) bool {
	select {
	case <-m.ctx.Done():
		return true
	case m.fileQueue <- task:
		return true
	default:
		return false
	}
}

func (m *Manager) noteRescan(dirBucket string) {
	m.rescan.Store(true)
	first := strings.SplitN(dirBucket, "-", 2)[0]
	if id, err := strconv.ParseInt(first, 10, 64); err == nil && id >= 0 {
		for {
			old := m.rescanFrom.Load()
			if id >= old || m.rescanFrom.CompareAndSwap(old, id) {
				break
			}
		}
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case task := <-m.fileQueue:
			s, release, ok := m.AcquireSession(task.sessionID)
			if !ok {
				continue
			}
			if s.Writer == nil {
				release()
				continue
			}
			err := s.IndexFileWithRetry(m.ctx, task.path)
			release()
			if err != nil {
				m.log.Warn("index live file", "session", task.sessionID, "file", task.path, "error", err)
				_ = m.store.AddIndexError(m.ctx, task.sessionID, task.path, err.Error())
			}
		}
	}
}

func (m *Manager) rescanLoop() {
	defer m.wg.Done()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.rescanRecent()
		}
	}
}

// discoveryLoop is a safety net for the root fsnotify watcher. A session
// directory can be created before fluxzy has written meta.json (or before the
// watcher is registered), so a create event can be missed entirely. Retry any
// directory that has not been opened yet.
func (m *Manager) discoveryLoop() {
	defer m.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.discoverMissedSessions()
		}
	}
}

func (m *Manager) discoverMissedSessions() {
	root := filepath.Join(m.cfg.FluxzyDataDir, "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	expectedID, _ := m.ExpectedSession()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !sessiondir.ValidID(id) {
			continue
		}
		m.mu.RLock()
		_, opened := m.sessions[id]
		_, known := m.known[id]
		m.mu.RUnlock()
		if opened || known {
			continue
		}
		dir := filepath.Join(root, id)
		// The expected session must be opened eagerly so the supervisor can
		// confirm the new capture. Historical directories are registered and
		// queued for the bounded scan pool instead.
		if expectedID != "" && id == expectedID {
			m.discoverNewSession(id, dir)
		} else {
			m.discoverArchived(id, dir)
		}
	}
}

func (m *Manager) rescanRecent() {
	if !m.rescan.Load() {
		return
	}
	id := m.ActiveSessionID()
	if id == "" || m.indexing.Load() {
		return
	}
	s, ok := m.GetSession(id)
	if !ok {
		return
	}
	m.rescan.Store(false)
	ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
	defer cancel()
	maxID, err := s.Index.MaxID(ctx)
	if err != nil {
		m.rescan.Store(true)
		return
	}
	if maxID < 0 {
		maxID = 0
	}
	root := filepath.Join(s.Dir, "exchanges")
	highest := maxID / 100
	start := highest
	if first := m.rescanFrom.Swap(math.MaxInt64); first != math.MaxInt64 {
		start = first / 100
	}
	if start > 0 {
		start--
	}
	if start < 0 {
		start = 0
	}
	if entries, readErr := os.ReadDir(root); readErr == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if bucket, ok := bucketStart(e.Name()); ok && bucket/100 > highest {
				highest = bucket / 100
			}
		}
	} else {
		m.rescan.Store(true)
		return
	}

	for bucket := start; bucket <= highest; bucket++ {
		select {
		case <-ctx.Done():
			m.noteRescan(fmt.Sprintf("%d-%d", bucket*100, bucket*100+100))
			return
		default:
		}
		dir := filepath.Join(root, fmt.Sprintf("%d-%d", bucket*100, bucket*100+100))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mpack") {
				continue
			}
			if !m.enqueueFile(fileTask{sessionID: id, path: filepath.Join(dir, e.Name())}) {
				m.noteRescan(fmt.Sprintf("%d-%d", bucket*100, bucket*100+100))
				return
			}
		}
	}
}

func bucketStart(name string) (int64, bool) {
	first := strings.SplitN(name, "-", 2)[0]
	id, err := strconv.ParseInt(first, 10, 64)
	return id, err == nil
}

func (m *Manager) onlineLoop() {
	defer m.wg.Done()
	check := func() {
		online := false
		if m.processRunning.Load() {
			m.proxyMu.RLock()
			addr := m.proxyAddr
			m.proxyMu.RUnlock()
			conn, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
			online = err == nil
			if conn != nil {
				_ = conn.Close()
			}
		}
		prev := m.online.Swap(online)
		if prev != online {
			m.refreshStatus()
			m.hub.Publish(live.Event{Type: "capture.status", Data: map[string]any{
				"online":          online,
				"activeSessionId": m.ActiveSessionID(),
			}})
		}
	}
	check()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

func readMetaRaw(dir string) (Meta, string, error) {
	var meta Meta
	path := filepath.Join(dir, "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return meta, "", err
	}
	if len(b) == 0 {
		return meta, "", errors.New("empty meta.json")
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, "", err
	}
	if meta.CaptureDate.IsZero() {
		info, err := os.Stat(dir)
		if err == nil {
			meta.CaptureDate = info.ModTime()
		}
	}
	return meta, string(b), nil
}
