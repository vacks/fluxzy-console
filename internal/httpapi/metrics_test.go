package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vacks/fluxzy-console/internal/config"
)

func TestMetricsAuthOptional(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	open := (&Handlers{deps: Deps{Cfg: config.Config{}}}).metricsAuth(next)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	open.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("without token from loopback: status %d", rec.Code)
	}

	remote := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	remote.RemoteAddr = "192.0.2.10:12345"
	rec = httptest.NewRecorder()
	open.ServeHTTP(rec, remote)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token from remote: status %d", rec.Code)
	}

	protected := (&Handlers{deps: Deps{Cfg: config.Config{MetricsToken: "s3cret"}}}).metricsAuth(next)

	cases := []struct {
		name   string
		header string
		value  string
		want   int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "wrong", header: "Authorization", value: "Bearer nope", want: http.StatusUnauthorized},
		{name: "bearer", header: "Authorization", value: "Bearer s3cret", want: http.StatusNoContent},
		{name: "explicit", header: "X-Metrics-Token", value: "s3cret", want: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
