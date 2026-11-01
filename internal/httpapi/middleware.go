package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/vacks/fluxzy-console/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxAuthSession
	ctxToken
)

const (
	sessionCookie = "fc_session"
	csrfHeader    = "X-CSRF-Token"
)

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxUser).(*store.User)
	return u
}

func currentAuth(r *http.Request) *store.AuthSession {
	a, _ := r.Context().Value(ctxAuthSession).(*store.AuthSession)
	return a
}

func (h *Handlers) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录", nil)
			return
		}
		u, as, err := h.deps.Auth.Authenticate(r.Context(), c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录", nil)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxAuthSession, as)
		ctx = context.WithValue(ctx, ctxToken, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handlers) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		as := currentAuth(r)
		if as == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录", nil)
			return
		}
		got := r.Header.Get(csrfHeader)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(as.CSRFToken)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_failed", "CSRF 校验失败，请刷新页面后重试", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || u.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || (u.Role != "admin" && u.Role != "operator") {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// Unwrap lets http.NewResponseController (used by WebSocket upgrades and
// http.ServeContent) reach the underlying ResponseWriter.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (h *Handlers) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/assets/") && sw.status < 400 {
			return
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"ip", clientIP(r, h.deps.Cfg.TrustProxyHeaders),
		}
		if u := currentUser(r); u != nil {
			attrs = append(attrs, "user", u.Username)
		}
		h.deps.Log.Info("http", attrs...)
	})
}

func (h *Handlers) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				h.deps.Log.Error("panic", "error", fmt.Sprint(rec), "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request, trustProxy bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Forwarded headers are only meaningful when the direct peer is a local or
	// private proxy. Otherwise a direct internet client could rotate
	// X-Forwarded-For to bypass the login limiter and forge audit IPs.
	if trustProxy && trustedProxyPeer(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			// Proxies append the peer they observed. The rightmost value is
			// therefore the one least likely to be attacker-controlled.
			for i := len(parts) - 1; i >= 0; i-- {
				candidate := strings.TrimSpace(parts[i])
				if len(candidate) <= 64 && net.ParseIP(candidate) != nil {
					return candidate
				}
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); len(xri) <= 64 && net.ParseIP(xri) != nil {
			return xri
		}
	}
	return host
}

func trustedProxyPeer(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func (h *Handlers) audit(r *http.Request, action, target string, detail any) {
	h.auditAs(r, currentUser(r), action, target, detail)
}

func (h *Handlers) auditAs(r *http.Request, actor *store.User, action, target string, detail any) {
	var uid *int64
	username := ""
	if actor != nil {
		id := actor.ID
		uid = &id
		username = actor.Username
	}
	detailText := ""
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			detailText = string(b)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.deps.Store.AddAudit(ctx, store.AuditEntry{
		UserID:    uid,
		Username:  username,
		Action:    action,
		Target:    target,
		Detail:    detailText,
		IP:        clientIP(r, h.deps.Cfg.TrustProxyHeaders),
		UserAgent: r.UserAgent(),
	}); err != nil {
		h.deps.Log.Warn("write audit", "error", err)
	}
}

func (h *Handlers) logger() *slog.Logger { return h.deps.Log }
