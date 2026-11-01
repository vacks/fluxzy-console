package rules

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vacks/fluxzy-console/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "console.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	live := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(live, []byte(DefaultYAML), 0o640); err != nil {
		t.Fatalf("write live rules: %v", err)
	}
	svc := &Service{
		cfg: Config{
			RulesFile:  live,
			StatusFile: filepath.Join(dir, ".rules-status.json"),
		},
		store: st,
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return svc, st, live
}

func TestSaveWritesPendingCandidateNotLiveFile(t *testing.T) {
	svc, _, live := newTestService(t)
	candidate := `rules:
  - name: 草稿
    filter:
      typeKind: AnyFilter
    actions:
      - typeKind: NoOpAction
`
	version, validation, err := svc.Save(context.Background(), candidate, "draft", nil)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("validation: %+v", validation)
	}
	if version == nil || version.Status != "pending" {
		t.Fatalf("version = %+v", version)
	}
	liveBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(liveBytes) != DefaultYAML {
		t.Fatalf("Save modified the watched live rules file:\n%s", liveBytes)
	}
	pendingBytes, err := os.ReadFile(svc.pendingPath())
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if string(pendingBytes) != candidate {
		t.Fatalf("pending = %q", pendingBytes)
	}
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Pending || status.YAML != candidate {
		t.Fatalf("status pending=%v yaml=%q", status.Pending, status.YAML)
	}
	if status.Hash != HashYAML(candidate) {
		t.Fatalf("hash = %q, want candidate hash", status.Hash)
	}
}

func TestReadCandidateFallsBackToLive(t *testing.T) {
	svc, _, _ := newTestService(t)
	text, _, pending, err := svc.ReadCandidate()
	if err != nil {
		t.Fatalf("ReadCandidate: %v", err)
	}
	if pending {
		t.Fatal("unexpected pending candidate")
	}
	if !strings.Contains(text, "AnyFilter") {
		t.Fatalf("candidate = %q", text)
	}
}
