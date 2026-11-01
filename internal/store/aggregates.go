package store

import (
	"context"
	"time"
)

type Aggregate struct {
	Bucket    int64  `json:"bucket"`
	SessionID string `json:"sessionId,omitempty"`
	Count     int64  `json:"count"`
	ReqBytes  int64  `json:"reqBytes"`
	RespBytes int64  `json:"respBytes"`
	Status2xx int64  `json:"status2xx"`
	Status3xx int64  `json:"status3xx"`
	Status4xx int64  `json:"status4xx"`
	Status5xx int64  `json:"status5xx"`
	Errors    int64  `json:"errors"`
	// Pending is populated by index totals only; it is not written to the
	// minute aggregate table.
	Pending int64 `json:"pending,omitempty"`
}

func (s *Store) AddAggregate(ctx context.Context, a Aggregate) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO aggregates (bucket, session_id, count, req_bytes, resp_bytes, status_2xx, status_3xx, status_4xx, status_5xx, errors)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(bucket, session_id) DO UPDATE SET
		   count = count + excluded.count,
		   req_bytes = req_bytes + excluded.req_bytes,
		   resp_bytes = resp_bytes + excluded.resp_bytes,
		   status_2xx = status_2xx + excluded.status_2xx,
		   status_3xx = status_3xx + excluded.status_3xx,
		   status_4xx = status_4xx + excluded.status_4xx,
		   status_5xx = status_5xx + excluded.status_5xx,
		   errors = errors + excluded.errors`,
		a.Bucket, a.SessionID, a.Count, a.ReqBytes, a.RespBytes,
		a.Status2xx, a.Status3xx, a.Status4xx, a.Status5xx, a.Errors)
	return err
}

func (s *Store) ListAggregates(ctx context.Context, sessionID string, since int64, limit int) ([]Aggregate, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1440
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT bucket, session_id, count, req_bytes, resp_bytes, status_2xx, status_3xx, status_4xx, status_5xx, errors
		 FROM aggregates
		 WHERE bucket >= ? AND (? = '' OR session_id = ?)
		 ORDER BY bucket ASC LIMIT ?`, since, sessionID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Aggregate, 0, 256)
	for rows.Next() {
		var a Aggregate
		if err := rows.Scan(&a.Bucket, &a.SessionID, &a.Count, &a.ReqBytes, &a.RespBytes,
			&a.Status2xx, &a.Status3xx, &a.Status4xx, &a.Status5xx, &a.Errors); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SumAggregates(ctx context.Context, sessionID string) (Aggregate, error) {
	a := Aggregate{SessionID: sessionID}
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(count),0), COALESCE(SUM(req_bytes),0), COALESCE(SUM(resp_bytes),0),
		        COALESCE(SUM(status_2xx),0), COALESCE(SUM(status_3xx),0), COALESCE(SUM(status_4xx),0),
		        COALESCE(SUM(status_5xx),0), COALESCE(SUM(errors),0)
		 FROM aggregates WHERE session_id = ?`, sessionID).
		Scan(&a.Count, &a.ReqBytes, &a.RespBytes, &a.Status2xx, &a.Status3xx, &a.Status4xx, &a.Status5xx, &a.Errors)
	return a, err
}

func (s *Store) PurgeAggregates(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM aggregates WHERE bucket < ?`, olderThan.Unix())
	return err
}
