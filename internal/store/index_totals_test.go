package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestIndexTotalsFollowUpserts(t *testing.T) {
	ctx := context.Background()
	idx, err := OpenIndex(filepath.Join(t.TempDir(), "hot-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.UpsertBatch(ctx, []ExchangeRow{{
		ID: 1, Status: 0, ReqSize: 10, RespSize: 0, Pending: true,
	}}); err != nil {
		t.Fatal(err)
	}
	a, pending, err := idx.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count != 1 || pending != 1 {
		t.Fatalf("after pending: count=%d pending=%d", a.Count, pending)
	}

	if err := idx.UpsertBatch(ctx, []ExchangeRow{{
		ID: 1, Status: 200, ReqSize: 10, RespSize: 20, Pending: false,
	}}); err != nil {
		t.Fatal(err)
	}
	a, pending, err = idx.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count != 1 || pending != 0 || a.Status2xx != 1 || a.RespBytes != 20 {
		t.Fatalf("after final: %+v pending=%d", a, pending)
	}

	if err := idx.UpsertBatch(ctx, []ExchangeRow{{
		ID: 2, Status: 404, ReqSize: 5, RespSize: 6,
	}}); err != nil {
		t.Fatal(err)
	}
	a, _, err = idx.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count != 2 || a.Status2xx != 1 || a.Status4xx != 1 || a.ReqBytes != 15 || a.RespBytes != 26 {
		t.Fatalf("after second exchange: %+v", a)
	}
}

// Reopening an existing index must not fail when the totals row is already
// present: migrations run on every OpenIndex, so the initializer has to be
// idempotent.
func TestIndexTotalsMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hot-reopen.db")

	idx, err := OpenIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.UpsertBatch(ctx, []ExchangeRow{
		{ID: 1, Status: 200, ReqSize: 10, RespSize: 20},
		{ID: 2, Status: 500, ReqSize: 5, RespSize: 6},
	}); err != nil {
		t.Fatal(err)
	}
	before, _, err := idx.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenIndex(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	after, _, err := reopened.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("totals changed across reopen: before=%+v after=%+v", before, after)
	}
	if after.Count != 2 || after.Status2xx != 1 || after.Status5xx != 1 {
		t.Fatalf("unexpected totals: %+v", after)
	}
	var version int
	if err := reopened.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != indexSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, indexSchemaVersion)
	}
}

func TestIndexHostCountsFollowChanges(t *testing.T) {
	ctx := context.Background()
	idx, err := OpenIndex(filepath.Join(t.TempDir(), "hot-hosts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	top := func() []HostCount {
		t.Helper()
		hosts, err := idx.TopHosts(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		return hosts
	}
	assertTop := func(want ...HostCount) {
		t.Helper()
		got := top()
		if len(got) != len(want) {
			t.Fatalf("top hosts = %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("top hosts = %+v, want %+v", got, want)
			}
		}
	}

	if err := idx.UpsertBatch(ctx, []ExchangeRow{
		{ID: 1, Host: "a.example", Status: 0, Pending: true},
		{ID: 2, Host: "b.example", Status: 200},
		{ID: 3, Host: "a.example", Status: 200},
	}); err != nil {
		t.Fatal(err)
	}
	assertTop(HostCount{Host: "a.example", Count: 2}, HostCount{Host: "b.example", Count: 1})

	// A pending -> final update keeps the same host and must not change counts.
	if err := idx.UpsertBatch(ctx, []ExchangeRow{{ID: 1, Host: "a.example", Status: 200}}); err != nil {
		t.Fatal(err)
	}
	assertTop(HostCount{Host: "a.example", Count: 2}, HostCount{Host: "b.example", Count: 1})

	// Moving an exchange to another host decrements the old host and increments
	// the new one.
	if err := idx.UpsertBatch(ctx, []ExchangeRow{{ID: 2, Host: "a.example", Status: 200}}); err != nil {
		t.Fatal(err)
	}
	assertTop(HostCount{Host: "a.example", Count: 3})

	if _, err := idx.db.ExecContext(ctx, `DELETE FROM exchanges WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	assertTop(HostCount{Host: "a.example", Count: 2})

	if _, err := idx.db.ExecContext(ctx, `DELETE FROM exchanges WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}
	assertTop()
}
