package live

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event is the envelope pushed over the live WebSocket. Exchange events are
// intentionally sampled per subscriber; stats and session events are not.
type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type Subscriber struct {
	Events chan Event

	maxPerSec int64
	paused    atomic.Bool
	lastSent  atomic.Int64
	dropped   atomic.Int64
	sent      atomic.Int64
}

func (s *Subscriber) Pause()         { s.paused.Store(true) }
func (s *Subscriber) Resume()        { s.paused.Store(false) }
func (s *Subscriber) Paused() bool   { return s.paused.Load() }
func (s *Subscriber) Dropped() int64 { return s.dropped.Load() }
func (s *Subscriber) Sent() int64    { return s.sent.Load() }

// offer applies a lightweight per-subscriber rate limit and never blocks the
// publisher. At 3k req/s the UI must not receive 3k rows/s; the full data is
// queryable through the REST API.
func (s *Subscriber) offer(ev Event) {
	if ev.Type == "exchange" {
		if s.paused.Load() {
			s.dropped.Add(1)
			return
		}
		interval := int64(time.Second) / s.maxPerSec
		now := time.Now().UnixNano()
		last := s.lastSent.Load()
		if last != 0 && now-last < interval {
			s.dropped.Add(1)
			return
		}
		select {
		case s.Events <- ev:
			s.lastSent.Store(now)
			s.sent.Add(1)
		default:
			s.dropped.Add(1)
		}
		return
	}
	// Control/stats events are not rate-limited and are not part of the
	// sent/dropped counters shown for live exchange traffic.
	select {
	case s.Events <- ev:
	default:
	}
}

type Hub struct {
	mu     sync.RWMutex
	subs   map[*Subscriber]struct{}
	bcast  chan Event
	closed atomic.Bool
}

func NewHub() *Hub {
	h := &Hub{
		subs:  make(map[*Subscriber]struct{}),
		bcast: make(chan Event, 8192),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for ev := range h.bcast {
		h.mu.RLock()
		for s := range h.subs {
			s.offer(ev)
		}
		h.mu.RUnlock()
	}
}

func (h *Hub) Publish(ev Event) {
	// Hold the read lock across the closed check and send so Close cannot
	// close the broadcast channel in between (send on closed channel panic).
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed.Load() {
		return
	}
	select {
	case h.bcast <- ev:
	default:
		// The hub is saturated. The database remains the source of truth and
		// clients can resync through the REST API.
	}
}

func (h *Hub) Subscribe(maxPerSec int) *Subscriber {
	if maxPerSec < 1 {
		maxPerSec = 20
	}
	s := &Subscriber{
		Events:    make(chan Event, 512),
		maxPerSec: int64(maxPerSec),
	}
	h.mu.Lock()
	if h.closed.Load() || len(h.subs) >= 128 {
		h.mu.Unlock()
		return nil
	}
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed.Swap(true) {
		return
	}
	close(h.bcast)
	for s := range h.subs {
		delete(h.subs, s)
	}
}
