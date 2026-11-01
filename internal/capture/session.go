package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vacks/fluxzy-console/internal/fluxzy/archive"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/store"
)

type Meta struct {
	CaptureDate       time.Time       `json:"captureDate"`
	FluxzyVersion     string          `json:"fluxzyVersion"`
	ArchiveVersion    string          `json:"archiveVersion"`
	CapturedSetting   json.RawMessage `json:"capturedSetting"`
	ResolvedEndPoints json.RawMessage `json:"resolvedEndPoints"`
	Tags              json.RawMessage `json:"tags"`
	Environment       json.RawMessage `json:"environmentInformation"`
}

type Session struct {
	ID   string
	Dir  string
	Meta Meta

	Index  *store.IndexStore
	Writer *Writer

	log          *slog.Logger
	indexed      atomic.Bool
	scanComplete atomic.Bool
	scanning     atomic.Int32
	refs         atomic.Int32
	lastUsed     atomic.Int64

	metaMu   sync.Mutex
	metaSeen map[int64][2]int64
}

var errExchangesNotReady = fmt.Errorf("exchanges directory is not ready")

func OpenSession(ctx context.Context, id, dir string, meta Meta, index *store.IndexStore, hub *live.Hub, agg *Aggregator, st *store.Store, log *slog.Logger) *Session {
	s := &Session{ID: id, Dir: dir, Meta: meta, Index: index, log: log}
	s.Writer = NewWriter(ctx, id, index, hub, agg, st, log)
	return s
}

// OpenArchivedSession opens a read-only view of a historical session. It does
// not start a Writer goroutine or background flush ticker.
func OpenArchivedSession(id, dir string, meta Meta, index *store.IndexStore, log *slog.Logger) *Session {
	return &Session{ID: id, Dir: dir, Meta: meta, Index: index, log: log}
}

func (s *Session) ScanComplete() bool { return s.scanComplete.Load() }

func (s *Session) Scanning() bool { return s.scanning.Load() > 0 }

func (s *Session) Touch() { s.lastUsed.Store(time.Now().UnixNano()) }

func (s *Session) IdleSince() time.Duration {
	last := s.lastUsed.Load()
	if last == 0 {
		return time.Duration(1<<62 - 1)
	}
	return time.Since(time.Unix(0, last))
}

func (s *Session) Close() {
	if s.Writer != nil {
		s.Writer.Stop()
	}
	if s.Index != nil {
		_ = s.Index.Close()
	}
}

// IndexFile decodes one exchange mpack and feeds it to the batch writer.
// Fluxzy rewrites the same file when the response progresses, so this is
// called several times for the same exchange; the upsert is idempotent.
const maxIndexFileBytes = 64 << 20

func (s *Session) IndexFile(ctx context.Context, path string, info fs.FileInfo) error {
	if info.Size() > maxIndexFileBytes {
		return fmt.Errorf("exchange file is too large: %d bytes", info.Size())
	}
	id, hasID := store.ParseExchangeIDFromPath(path)
	mtime, size := info.ModTime().UnixNano(), info.Size()
	if hasID {
		s.metaMu.Lock()
		cached, ok := s.metaSeen[id]
		s.metaMu.Unlock()
		if ok && cached == [2]int64{mtime, size} {
			return nil
		}
		if dbMtime, dbSize, pending, found, err := s.Index.GetFileMeta(ctx, id); err == nil && found && !pending &&
			dbMtime == mtime && dbSize == size {
			s.rememberMeta(id, mtime, size)
			return nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ex, err := archive.DecodeExchange(data)
	if err != nil {
		return err
	}
	row := ToRow(ex, mtime, size)
	s.Writer.Enqueue(row)
	if hasID {
		s.rememberMeta(id, mtime, size)
	}
	return nil
}

func (s *Session) rememberMeta(id, mtime, size int64) {
	s.metaMu.Lock()
	if s.metaSeen == nil {
		s.metaSeen = make(map[int64][2]int64)
	}
	if len(s.metaSeen) > 200_000 {
		s.metaSeen = make(map[int64][2]int64)
	}
	s.metaSeen[id] = [2]int64{mtime, size}
	s.metaMu.Unlock()
}

func (s *Session) IndexFileWithRetry(ctx context.Context, path string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		// Give fluxzy a moment to finish the truncate+write sequence.
		if info.Size() == 0 {
			lastErr = fmt.Errorf("empty exchange file")
		} else {
			if err := s.IndexFile(ctx, path, info); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(60+attempt*40) * time.Millisecond):
		}
	}
	return lastErr
}

func (s *Session) FullScan(ctx context.Context, aggregate bool) error {
	if s.indexed.Swap(true) {
		return nil
	}
	success := false
	defer func() {
		if success {
			s.scanComplete.Store(true)
		} else {
			s.indexed.Store(false)
			s.scanComplete.Store(false)
		}
	}()

	root := filepath.Join(s.Dir, "exchanges")
	ready := false
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !ready {
		if _, err := os.Stat(root); err == nil {
			ready = true
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errExchangesNotReady
		case <-time.After(100 * time.Millisecond):
		}
	}

	paths := make(chan string, 512)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}
	workers := 4
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range paths {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if err := s.IndexFileWithRetry(ctx, p); err != nil {
					recordErr(err)
					if s.log != nil {
						s.log.Warn("index exchange", "session", s.ID, "file", p, "error", err)
					}
				}
			}
		}()
	}

	var n int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			recordErr(walkErr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".mpack") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case paths <- path:
			n++
			if n%2000 == 0 && s.log != nil {
				s.log.Info("index progress", "session", s.ID, "files", n)
			}
		}
		return nil
	})
	close(paths)
	wg.Wait()
	recordErr(err)

	flushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	s.Writer.FlushAndWait(flushCtx)
	cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	errMu.Lock()
	scanErr := firstErr
	errMu.Unlock()
	if scanErr != nil {
		return scanErr
	}
	if aggregate {
		if agg, aggErr := s.Index.SettledStats(ctx); aggErr == nil {
			pendingIDs, _ := s.Index.PendingIDs(ctx)
			s.Writer.ResetAggregator(agg, pendingIDs)
		}
	}
	s.Writer.RefreshStats()
	success = true
	return nil
}

func ToRow(ex *archive.Exchange, mtime, size int64) store.ExchangeRow {
	started := ex.Metrics.StartedAt()
	if started.IsZero() {
		started = time.Now()
	}
	host := ex.Host()
	url := ex.FullURL()
	agent := ""
	if ex.Agent != nil {
		agent = ex.Agent.FriendlyName
	}
	tags := ex.Tags
	if tags == nil {
		tags = []string{}
	}
	return store.ExchangeRow{
		ID:           int64(ex.ID),
		ConnectionID: ex.ConnectionID,
		Method:       ex.Request.Method,
		Scheme:       ex.Request.Scheme,
		Host:         host,
		Port:         ex.KnownPort,
		Path:         ex.PathOnly(),
		URL:          url,
		Status:       ex.StatusCode(),
		ContentType:  ex.ContentType(),
		ReqSize:      ex.Metrics.TotalSent,
		RespSize:     ex.Metrics.TotalReceived,
		DurationMs:   ex.Metrics.Duration().Milliseconds(),
		StartedAt:    started.UnixMilli(),
		Pending:      ex.Pending,
		IsWebSocket:  ex.IsWebSocket,
		HasError:     len(ex.ClientErrors) > 0,
		Tags:         tags,
		Agent:        agent,
		HTTPVersion:  ex.HTTPVersion,
		EgressIP:     ex.EgressIP,
		Comment:      ex.Comment,
		Secure:       ex.Secure,
		FileMTime:    mtime,
		FileSize:     size,
	}
}

func ReadMeta(dir string) (Meta, error) {
	var m Meta
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, nil
}

func (m Meta) Raw() string {
	b, _ := json.Marshal(m)
	return string(b)
}
