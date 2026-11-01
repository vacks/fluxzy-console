package sessiondir

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNextCreatesUniqueDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	m := New(root)
	now := time.Date(2026, 11, 1, 3, 15, 0, 0, time.UTC)
	id1, dir1, err := m.Next(now)
	if err != nil {
		t.Fatal(err)
	}
	id2, dir2, err := m.Next(now)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 || dir1 == dir2 {
		t.Fatalf("duplicate session: %s %s", id1, id2)
	}
	if !ValidID(id1) || !ValidID(id2) {
		t.Fatalf("invalid id: %s %s", id1, id2)
	}
	if info, err := os.Stat(dir1); err != nil || !info.IsDir() {
		t.Fatalf("dir1 missing: %v", err)
	}
}
