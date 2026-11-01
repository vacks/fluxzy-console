package runtimeconfig

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/store"
)

func testDefaults() Config {
	return Defaults(config.Config{Startup: config.FluxzyStartupDefaults{
		ListenAddr:             "0.0.0.0:443",
		Mode:                   "ReverseSecure",
		ReversePort:            443,
		MaxUpstreamConnections: 16,
		RequestBuffer:          4096,
	}})
}

func TestAppliedConfigDoesNotFollowPendingSave(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "console.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := New(ctx, st, testDefaults(), log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	next := svc.Effective()
	next.MaxUpstreamConnections = 32
	if err := svc.Update(ctx, next, "test"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !svc.Pending() {
		t.Fatal("expected pending config")
	}
	if got := svc.Applied().MaxUpstreamConnections; got != 16 {
		t.Fatalf("applied connections = %d, want previous value 16", got)
	}
	appliedSpec, appliedHash := svc.AppliedSpecWithHash()
	if appliedSpec.MaxUpstreamConnections != 16 {
		t.Fatalf("applied spec connections = %d", appliedSpec.MaxUpstreamConnections)
	}
	if appliedHash == svc.Hash() {
		t.Fatal("applied hash should differ from pending hash")
	}

	if err := svc.MarkApplied(ctx, svc.Hash(), "session-1"); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	if svc.Pending() {
		t.Fatal("pending should clear after MarkApplied")
	}
	if got := svc.Applied().MaxUpstreamConnections; got != 32 {
		t.Fatalf("applied connections = %d, want 32", got)
	}
}
