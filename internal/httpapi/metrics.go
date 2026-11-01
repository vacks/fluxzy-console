package httpapi

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// metricsAuth optionally protects /metrics with a bearer token. Prometheus can
// send it as `authorization: {credentials: <token>}`; X-Metrics-Token is also
// accepted for simple probes. An empty token keeps the previous behavior and
// leaves the endpoint open, so existing scrapers are unaffected.
func (h *Handlers) metricsAuth(next http.Handler) http.Handler {
	token := strings.TrimSpace(h.deps.Cfg.MetricsToken)
	if token == "" {
		// With no token, only a loopback scraper may read metrics. A
		// reverse-proxied or LAN request must configure a token.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := net.ParseIP(clientIP(r, h.deps.Cfg.TrustProxyHeaders))
			if ip == nil || !ip.IsLoopback() {
				writeError(w, http.StatusUnauthorized, "metrics_local_only", "指标接口仅允许回环地址访问", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Metrics-Token")
		if got == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				got = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "需要指标访问令牌", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
