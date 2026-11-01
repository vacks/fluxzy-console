package binary

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolverUsesBundledCLI(t *testing.T) {
	dir := t.TempDir()
	bundled := filepath.Join(dir, "bundled")
	if err := os.MkdirAll(bundled, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(bundled, "fluxzy")
	script := "#!/bin/sh\necho '1.39.51+abc12345'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(bundled, "1.39.51.62629")
	info, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != SourceBundled || info.BaseVersion != "1.39.51" || info.ReleaseTag != "1.39.51.62629" {
		t.Fatalf("%+v", info)
	}
}

func TestResolverRejectsMissingCLI(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "missing"), "1.39.51.62629")
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("expected an error for a missing bundled CLI")
	}
}
