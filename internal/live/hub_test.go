package live

import "testing"

func TestSubscriberCountsExchangeEventsOnly(t *testing.T) {
	s := &Subscriber{Events: make(chan Event, 8), maxPerSec: 1}

	s.offer(Event{Type: "stats"})
	if s.Sent() != 0 || s.Dropped() != 0 {
		t.Fatalf("stats event must not affect counters: sent=%d dropped=%d", s.Sent(), s.Dropped())
	}

	s.offer(Event{Type: "exchange"})
	s.offer(Event{Type: "exchange"})
	if s.Sent() != 1 {
		t.Fatalf("sent=%d, want 1", s.Sent())
	}
	if s.Dropped() != 1 {
		t.Fatalf("dropped=%d, want 1", s.Dropped())
	}

	s.Pause()
	s.offer(Event{Type: "exchange"})
	if s.Dropped() != 2 {
		t.Fatalf("paused exchange dropped=%d, want 2", s.Dropped())
	}
}
