package capture

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/store"
)

type Writer struct {
	ctx       context.Context
	cancel    context.CancelFunc
	sessionID string
	index     *store.IndexStore
	hub       *live.Hub
	agg       *Aggregator
	store     *store.Store
	log       *slog.Logger

	ch       chan store.ExchangeRow
	flushReq chan chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	seenMu sync.Mutex
	seen   map[int64][2]int64
	sendMu sync.RWMutex
}

func NewWriter(parent context.Context, sessionID string, index *store.IndexStore, hub *live.Hub, agg *Aggregator, st *store.Store, log *slog.Logger) *Writer {
	ctx, cancel := context.WithCancel(parent)
	w := &Writer{
		ctx:       ctx,
		cancel:    cancel,
		sessionID: sessionID,
		index:     index,
		hub:       hub,
		agg:       agg,
		store:     st,
		log:       log,
		ch:        make(chan store.ExchangeRow, 4096),
		flushReq:  make(chan chan struct{}, 1),
		done:      make(chan struct{}),
		seen:      make(map[int64][2]int64),
	}
	go w.run()
	return w
}

func (w *Writer) Enqueue(row store.ExchangeRow) {
	w.sendMu.RLock()
	defer w.sendMu.RUnlock()
	if row.ID > 0 {
		key := [2]int64{row.FileMTime, row.FileSize}
		w.seenMu.Lock()
		if prev, ok := w.seen[row.ID]; ok && prev == key {
			w.seenMu.Unlock()
			return
		}
		if len(w.seen) > 100_000 {
			w.seen = make(map[int64][2]int64)
		}
		w.seen[row.ID] = key
		w.seenMu.Unlock()
	}
	if w.ctx.Err() != nil {
		return
	}
	select {
	case w.ch <- row:
	case <-w.ctx.Done():
	}
}

func (w *Writer) Stop() {
	w.stopOnce.Do(func() {
		// Block new sends before cancelling so a producer cannot enqueue after
		// the writer's shutdown drain has finished.
		w.sendMu.Lock()
		w.cancel()
		w.sendMu.Unlock()
		<-w.done
	})
}

// FlushAndWait asks the writer goroutine to commit its current batch. It is
// used after a full scan so the session counters are immediately accurate.
func (w *Writer) FlushAndWait(ctx context.Context) {
	done := make(chan struct{})
	select {
	case w.flushReq <- done:
	case <-w.ctx.Done():
		return
	case <-ctx.Done():
		return
	}
	select {
	case <-done:
	case <-w.ctx.Done():
	case <-ctx.Done():
	}
}

func (w *Writer) RefreshStats() {
	w.refreshStats()
}

func (w *Writer) ResetAggregator(a store.Aggregate, pendingIDs []int64) {
	if w.agg != nil {
		w.agg.Reset(w.sessionID, a, pendingIDs)
	}
}

func (w *Writer) run() {
	defer close(w.done)

	flushTicker := time.NewTicker(150 * time.Millisecond)
	defer flushTicker.Stop()
	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	batch := make([]store.ExchangeRow, 0, 512)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		rows := batch
		batch = make([]store.ExchangeRow, 0, 512)

		backoff := 100 * time.Millisecond
		for {
			ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
			err := w.index.UpsertBatch(ctx, rows)
			cancel()
			if err == nil {
				break
			}
			if w.log != nil {
				w.log.Error("write exchange batch", "session", w.sessionID, "rows", len(rows), "error", err)
			}
			if w.ctx.Err() != nil {
				// A final best-effort attempt during shutdown, then make the
				// loss visible instead of silently returning.
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				finalErr := w.index.UpsertBatch(ctx, rows)
				cancel()
				if finalErr != nil && w.log != nil {
					w.log.Error("dropping exchange batch during shutdown",
						"session", w.sessionID, "rows", len(rows), "error", finalErr)
				}
				return
			}
			select {
			case <-w.ctx.Done():
			case <-time.After(backoff):
			}
			if backoff < 2*time.Second {
				backoff *= 2
			}
		}

		if w.agg != nil {
			w.agg.AddBatch(w.sessionID, rows)
		}
		if w.hub != nil {
			for _, r := range rows {
				w.hub.Publish(live.Event{Type: "exchange", SessionID: w.sessionID, Data: r})
			}
		}
	}

	for {
		select {
		case <-w.ctx.Done():
			for {
				select {
				case r := <-w.ch:
					batch = append(batch, r)
					if len(batch) >= 512 {
						flush()
					}
				default:
					flush()
					w.refreshStats()
					return
				}
			}
		case done := <-w.flushReq:
		drain:
			for {
				select {
				case r := <-w.ch:
					batch = append(batch, r)
					if len(batch) >= 512 {
						flush()
					}
				default:
					break drain
				}
			}
			flush()
			close(done)
		case r := <-w.ch:
			batch = append(batch, r)
			if len(batch) >= 512 {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case <-statsTicker.C:
			w.refreshStats()
		}
	}
}

func (w *Writer) refreshStats() {
	if w.store == nil || w.index == nil || w.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, 15*time.Second)
	defer cancel()
	totals, _, err := w.index.Totals(ctx)
	if err != nil {
		return
	}
	_ = w.store.UpdateCaptureSessionIndexStats(ctx, w.sessionID, totals.Count,
		totals.ReqBytes+totals.RespBytes, time.Now().UnixMilli())
}
