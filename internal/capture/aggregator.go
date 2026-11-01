package capture

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/vacks/fluxzy-console/internal/store"
)

type Stats struct {
	SessionID   string  `json:"sessionId"`
	Total       int64   `json:"total"`
	Pending     int64   `json:"pending"`
	Errors      int64   `json:"errors"`
	ReqBytes    int64   `json:"reqBytes"`
	RespBytes   int64   `json:"respBytes"`
	Status2xx   int64   `json:"status2xx"`
	Status3xx   int64   `json:"status3xx"`
	Status4xx   int64   `json:"status4xx"`
	Status5xx   int64   `json:"status5xx"`
	RPS         float64 `json:"rps"`
	BytesPerSec float64 `json:"bytesPerSec"`
	LastEventAt int64   `json:"lastEventAt"`
}

type secondStat struct {
	sec   int64
	count int64
	bytes int64
}

type sessionAgg struct {
	minute int64

	// current minute counters
	count          int64
	reqBytes       int64
	respBytes      int64
	s2, s3, s4, s5 int64
	errors         int64
	pending        int64

	// pendingIDs tracks exchanges written as pending but not yet finalized.
	pendingIDs map[int64]struct{}

	// lifetime counters
	total              int64
	ltReq              int64
	ltResp             int64
	ltErrors           int64
	lt2, lt3, lt4, lt5 int64

	window    [8]secondStat
	lastEvent int64
}

type Aggregator struct {
	mu       sync.Mutex
	sessions map[string]*sessionAgg
	store    *store.Store
	log      *slog.Logger
}

func NewAggregator(st *store.Store, log *slog.Logger) *Aggregator {
	return &Aggregator{sessions: make(map[string]*sessionAgg), store: st, log: log}
}

func (a *Aggregator) AddBatch(sessionID string, rows []store.ExchangeRow) {
	if len(rows) == 0 {
		return
	}
	if len(rows) > 1 {
		seen := make(map[int64]int, len(rows))
		deduped := make([]store.ExchangeRow, 0, len(rows))
		for _, row := range rows {
			if row.ID > 0 {
				if idx, ok := seen[row.ID]; ok {
					deduped[idx] = row
					continue
				}
				seen[row.ID] = len(deduped)
			}
			deduped = append(deduped, row)
		}
		rows = deduped
	}
	now := time.Now()
	minute := now.Unix() / 60
	sec := now.Unix()

	a.mu.Lock()
	agg := a.sessions[sessionID]
	if agg == nil {
		agg = &sessionAgg{minute: minute}
		a.sessions[sessionID] = agg
	}
	var snapshot *aggregateSnapshot
	if agg.minute != minute {
		snapshot = a.snapshotLocked(sessionID, agg)
		agg.resetMinute(minute)
	}

	for _, r := range rows {
		// Pending rows are rewritten into final rows later. Counting both
		// versions inflates totals/status/bytes, so only transitions are
		// tracked here; the initial set is seeded from the index after scan.
		if r.Pending {
			if agg.pendingIDs == nil {
				agg.pendingIDs = make(map[int64]struct{})
			}
			if _, seen := agg.pendingIDs[r.ID]; !seen {
				agg.pendingIDs[r.ID] = struct{}{}
				agg.pending++
			}
			continue
		}
		if agg.pendingIDs != nil {
			if _, ok := agg.pendingIDs[r.ID]; ok {
				delete(agg.pendingIDs, r.ID)
				if agg.pending > 0 {
					agg.pending--
				}
			}
		}
		agg.total++
		agg.ltReq += r.ReqSize
		agg.ltResp += r.RespSize
		agg.count++
		agg.reqBytes += r.ReqSize
		agg.respBytes += r.RespSize
		if r.HasError {
			agg.errors++
			agg.ltErrors++
		}
		switch {
		case r.Status >= 500:
			agg.s5++
			agg.lt5++
		case r.Status >= 400:
			agg.s4++
			agg.lt4++
		case r.Status >= 300:
			agg.s3++
			agg.lt3++
		case r.Status >= 200:
			agg.s2++
			agg.lt2++
		}

		idx := int(sec % int64(len(agg.window)))
		w := &agg.window[idx]
		if w.sec != sec {
			w.sec = sec
			w.count = 0
			w.bytes = 0
		}
		w.count++
		w.bytes += r.ReqSize + r.RespSize
	}
	agg.lastEvent = now.UnixMilli()
	a.mu.Unlock()
	if snapshot != nil {
		a.writeSnapshot(*snapshot)
	}
}

// Reset seeds the lifetime counters from the persisted index. It is called
// after the initial scan so a console restart does not lose historical counts.
func (a *Aggregator) Reset(sessionID string, agg store.Aggregate, pendingIDs []int64) {
	now := time.Now()
	minute := now.Unix() / 60
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sessionID]
	if s == nil {
		s = &sessionAgg{}
		a.sessions[sessionID] = s
	}
	s.minute = minute
	s.total = agg.Count
	s.ltReq = agg.ReqBytes
	s.ltResp = agg.RespBytes
	s.lt2 = agg.Status2xx
	s.lt3 = agg.Status3xx
	s.lt4 = agg.Status4xx
	s.lt5 = agg.Status5xx
	s.ltErrors = agg.Errors
	s.pendingIDs = make(map[int64]struct{}, len(pendingIDs))
	for _, id := range pendingIDs {
		s.pendingIDs[id] = struct{}{}
	}
	if pendingIDs == nil {
		s.pending = agg.Pending
	} else {
		s.pending = int64(len(pendingIDs))
	}
	s.count = 0
	s.reqBytes = 0
	s.respBytes = 0
	s.s2, s.s3, s.s4, s.s5 = 0, 0, 0, 0
	s.errors = 0
	s.lastEvent = 0
	s.window = [8]secondStat{}
}

func (a *Aggregator) Remove(sessionID string) {
	a.mu.Lock()
	delete(a.sessions, sessionID)
	a.mu.Unlock()
}

func (a *Aggregator) Snapshot(sessionID string) Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	agg := a.sessions[sessionID]
	if agg == nil {
		return Stats{SessionID: sessionID}
	}
	now := time.Now().Unix()
	var count, bytes int64
	for _, w := range agg.window {
		if now-w.sec < int64(len(agg.window)) {
			count += w.count
			bytes += w.bytes
		}
	}
	window := float64(len(agg.window))
	return Stats{
		SessionID:   sessionID,
		Total:       agg.total,
		Pending:     agg.pending,
		Errors:      agg.ltErrors,
		ReqBytes:    agg.ltReq,
		RespBytes:   agg.ltResp,
		Status2xx:   agg.lt2,
		Status3xx:   agg.lt3,
		Status4xx:   agg.lt4,
		Status5xx:   agg.lt5,
		RPS:         float64(count) / window,
		BytesPerSec: float64(bytes) / window,
		LastEventAt: agg.lastEvent,
	}
}

func (a *Aggregator) FlushLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.flushAll()
			return
		case <-t.C:
			a.flushAll()
		}
	}
}

type aggregateSnapshot struct {
	sessionID string
	minute    int64
	count     int64
	reqBytes  int64
	respBytes int64
	s2, s3    int64
	s4, s5    int64
	errors    int64
}

func (a *Aggregator) flushAll() {
	a.mu.Lock()
	snapshots := make([]aggregateSnapshot, 0, len(a.sessions))
	for id, agg := range a.sessions {
		if snap := a.snapshotLocked(id, agg); snap != nil {
			snapshots = append(snapshots, *snap)
		}
	}
	a.mu.Unlock()
	for _, snap := range snapshots {
		a.writeSnapshot(snap)
	}
}

func (a *Aggregator) snapshotLocked(sessionID string, agg *sessionAgg) *aggregateSnapshot {
	if agg.count == 0 || a.store == nil {
		return nil
	}
	snap := &aggregateSnapshot{
		sessionID: sessionID,
		minute:    agg.minute,
		count:     agg.count,
		reqBytes:  agg.reqBytes,
		respBytes: agg.respBytes,
		s2:        agg.s2,
		s3:        agg.s3,
		s4:        agg.s4,
		s5:        agg.s5,
		errors:    agg.errors,
	}
	agg.count = 0
	agg.reqBytes = 0
	agg.respBytes = 0
	agg.s2, agg.s3, agg.s4, agg.s5 = 0, 0, 0, 0
	agg.errors = 0
	return snap
}

func (a *Aggregator) writeSnapshot(snap aggregateSnapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := a.store.AddAggregate(ctx, store.Aggregate{
		Bucket:    snap.minute * 60,
		SessionID: snap.sessionID,
		Count:     snap.count,
		ReqBytes:  snap.reqBytes,
		RespBytes: snap.respBytes,
		Status2xx: snap.s2,
		Status3xx: snap.s3,
		Status4xx: snap.s4,
		Status5xx: snap.s5,
		Errors:    snap.errors,
	})
	cancel()
	if err == nil {
		return
	}
	if a.log != nil {
		a.log.Warn("flush aggregate", "session", snap.sessionID, "error", err,
			"count", snap.count, "minute", snap.minute)
	}
	// Put the counters back when the minute has not advanced, so a transient
	// SQLite busy error does not silently lose them.
	a.mu.Lock()
	agg := a.sessions[snap.sessionID]
	if agg == nil {
		agg = &sessionAgg{minute: snap.minute}
		a.sessions[snap.sessionID] = agg
	}
	if agg.minute == snap.minute {
		agg.count += snap.count
		agg.reqBytes += snap.reqBytes
		agg.respBytes += snap.respBytes
		agg.s2 += snap.s2
		agg.s3 += snap.s3
		agg.s4 += snap.s4
		agg.s5 += snap.s5
		agg.errors += snap.errors
	}
	a.mu.Unlock()
}

func (s *sessionAgg) resetMinute(minute int64) {
	s.minute = minute
	s.count = 0
	s.reqBytes = 0
	s.respBytes = 0
	s.s2, s.s3, s.s4, s.s5 = 0, 0, 0, 0
	s.errors = 0
}
