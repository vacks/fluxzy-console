package supervisor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMergeEnvReplacesDuplicates(t *testing.T) {
	t.Setenv("HOME", "/inherited")
	env := mergeEnv(map[string]string{"HOME": "/override", "TMPDIR": "/tmp"})
	var homes []string
	for _, entry := range env {
		if strings.HasPrefix(entry, "HOME=") {
			homes = append(homes, entry)
		}
	}
	if len(homes) != 1 || homes[0] != "HOME=/override" {
		t.Fatalf("HOME entries = %v", homes)
	}
}

func TestCrashLoopThreshold(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(Options{Log: nil})
	s.ctx = ctx
	s.spec = StartSpec{}
	r := &run{generation: 1, startedAt: time.Now()}
	s.run = r
	s.state = StateStarting

	for i := 0; i < 5; i++ {
		s.run = r
		s.handleExit(r, nil, 1)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateFailed {
		t.Fatalf("state = %s, want failed", s.state)
	}
	if s.status.ConsecutiveFailures < 5 {
		t.Fatalf("consecutive failures = %d", s.status.ConsecutiveFailures)
	}
}

func TestRotateDoesNotStartAStoppedProcess(t *testing.T) {
	s := New(Options{Log: nil})
	s.state = StateStopped
	if err := s.Rotate(context.Background(), "disk_low"); err == nil {
		t.Fatal("Rotate on a stopped process should fail")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateStopped || s.run != nil {
		t.Fatalf("state=%s run=%v", s.state, s.run)
	}
}

func TestLogBufferRing(t *testing.T) {
	b := NewLogBuffer(2, nil, nil)
	b.append("stdout", "one")
	b.append("stdout", "two")
	b.append("stdout", "three")
	lines := b.Tail(0, 2)
	if len(lines) != 2 || lines[0].Text != "two" || lines[1].Text != "three" {
		t.Fatalf("tail = %+v", lines)
	}
	last := b.LastSeq()
	if b.ReadySince(last - 1) {
		t.Fatalf("unexpected readiness for ordinary lines")
	}
	b.append("stdout", "Listen on 0.0.0.0:443")
	if !b.ReadySince(last) {
		t.Fatal("readiness banner not detected")
	}
}
