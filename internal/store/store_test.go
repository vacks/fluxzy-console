package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLegacyDatabaseIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	_, err = Open(path)
	if !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("Open error = %v, want ErrLegacySchema", err)
	}
}

func TestDeleteCaptureSessionCascades(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "cascade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertCaptureSession(ctx, CaptureSession{ID: "s1", Dir: "/tmp/s1", CaptureDate: 1, Status: "indexed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAggregate(ctx, Aggregate{Bucket: 60, SessionID: "s1", Count: 2}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddIndexError(ctx, "s1", "a.mpack", "bad"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteCaptureSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"aggregates", "index_errors", "capture_sessions"} {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE session_id = ?", "s1").Scan(&n); err != nil {
			// capture_sessions uses id, not session_id.
			if table == "capture_sessions" {
				if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM capture_sessions WHERE id = ?", "s1").Scan(&n); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Fatal(err)
			}
		}
		if n != 0 {
			t.Fatalf("%s still has %d rows", table, n)
		}
	}
}

func TestListCaptureSessionsPage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 1; i <= 5; i++ {
		status := "indexed"
		if i <= 2 {
			status = "expired"
		}
		id := "s" + string(rune('0'+i))
		if err := st.UpsertCaptureSession(ctx, CaptureSession{
			ID: id, Dir: "/tmp/" + id, CaptureDate: int64(i), Status: status,
		}); err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := st.ListCaptureSessionsPage(ctx, 2, 0, "", "", "s5")
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(items) != 2 || items[0].ID != "s5" || items[1].ID != "s4" {
		t.Fatalf("page 1 = total %d items %+v", total, items)
	}
	items, _, err = st.ListCaptureSessionsPage(ctx, 2, 2, "", "", "s5")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "s3" || items[1].ID != "s2" {
		t.Fatalf("page 2 items = %+v", items)
	}
	items, total, err = st.ListCaptureSessionsPage(ctx, 25, 0, "S1", "", "s5")
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != "s1" {
		t.Fatalf("search = total %d items %+v err %v", total, items, err)
	}
	_, total, err = st.ListCaptureSessionsPage(ctx, 25, 0, "", "live", "s5")
	if err != nil || total != 1 {
		t.Fatalf("live total = %d err %v", total, err)
	}
	_, total, err = st.ListCaptureSessionsPage(ctx, 25, 0, "", "archived", "s5")
	if err != nil || total != 2 {
		t.Fatalf("archived total = %d err %v", total, err)
	}
	_, total, err = st.ListCaptureSessionsPage(ctx, 25, 0, "", "sampled", "s5")
	if err != nil || total != 2 {
		t.Fatalf("sampled total = %d err %v", total, err)
	}
}

func TestAddIndexErrorDeduplicates(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "errors.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AddIndexError(ctx, "s1", "a.mpack", "first"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddIndexError(ctx, "s1", "a.mpack", "second"); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountIndexErrors(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("index errors = %d, want 1", n)
	}
}

func TestFreshDatabaseHasSchemaMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var version int
	if err := st.DB().QueryRowContext(context.Background(), `SELECT version FROM schema_meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("version = %d", version)
	}
}

func TestListAuditPage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, e := range []AuditEntry{
		{Username: "alice", Action: "auth.login", Target: "alice", IP: "10.0.0.1"},
		{Username: "bob", Action: "rules.save", Target: "r1", IP: "10.0.0.2"},
		{Username: "carol", Action: "session.delete", Target: "s1", IP: "10.0.0.3"},
	} {
		if err := st.AddAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := st.ListAuditPage(ctx, 2, 0, "", "")
	if err != nil || total != 3 || len(items) != 2 || items[0].Username != "carol" {
		t.Fatalf("page1 total=%d items=%+v err=%v", total, items, err)
	}
	items, total, err = st.ListAuditPage(ctx, 20, 0, "bob", "")
	if err != nil || total != 1 || items[0].Action != "rules.save" {
		t.Fatalf("search total=%d items=%+v err=%v", total, items, err)
	}
	_, total, err = st.ListAuditPage(ctx, 20, 0, "", "session.delete")
	if err != nil || total != 1 {
		t.Fatalf("action total=%d err=%v", total, err)
	}
	actions, err := st.ListAuditActions(ctx)
	if err != nil || len(actions) != 3 {
		t.Fatalf("actions=%v err=%v", actions, err)
	}
}
