package capture

import (
	"testing"

	"github.com/vacks/fluxzy-console/internal/store"
)

func TestAggregatorCountsFinalRowsOnce(t *testing.T) {
	a := NewAggregator(nil, nil)
	a.AddBatch("s", []store.ExchangeRow{
		{ID: 1, Pending: true},
		{ID: 1, Status: 200, ReqSize: 10, RespSize: 20},
		{ID: 2, Status: 404, ReqSize: 5, RespSize: 6},
	})
	stats := a.Snapshot("s")
	if stats.Total != 2 {
		t.Fatalf("total=%d, want 2 unique completed exchanges", stats.Total)
	}
	if stats.Status2xx != 1 || stats.Status4xx != 1 {
		t.Fatalf("status2xx=%d status4xx=%d", stats.Status2xx, stats.Status4xx)
	}
	if stats.ReqBytes != 15 || stats.RespBytes != 26 {
		t.Fatalf("reqBytes=%d respBytes=%d", stats.ReqBytes, stats.RespBytes)
	}
	if stats.Pending != 0 {
		t.Fatalf("pending=%d, want 0 after the pending row was finalized", stats.Pending)
	}

	a.AddBatch("s", []store.ExchangeRow{{ID: 3, Pending: true}})
	if got := a.Snapshot("s").Pending; got != 1 {
		t.Fatalf("pending=%d, want 1", got)
	}
	a.AddBatch("s", []store.ExchangeRow{{ID: 3, Status: 200, ReqSize: 1, RespSize: 2}})
	if got := a.Snapshot("s").Pending; got != 0 {
		t.Fatalf("pending=%d, want 0", got)
	}
}

func TestAggregatorResetSeedsPendingSet(t *testing.T) {
	a := NewAggregator(nil, nil)
	// SettledStats excludes pending rows from Count/bytes.
	a.Reset("s", store.Aggregate{Count: 3, Pending: 2}, []int64{10, 11})
	stats := a.Snapshot("s")
	if stats.Total != 3 || stats.Pending != 2 {
		t.Fatalf("total=%d pending=%d", stats.Total, stats.Pending)
	}
	a.AddBatch("s", []store.ExchangeRow{{ID: 10, Status: 200}, {ID: 10, Status: 200}})
	stats = a.Snapshot("s")
	if stats.Pending != 1 || stats.Total != 4 {
		t.Fatalf("pending=%d total=%d after first final row", stats.Pending, stats.Total)
	}
	a.AddBatch("s", []store.ExchangeRow{{ID: 11, Status: 200}})
	stats = a.Snapshot("s")
	if stats.Pending != 0 || stats.Total != 5 {
		t.Fatalf("pending=%d total=%d", stats.Pending, stats.Total)
	}
}
