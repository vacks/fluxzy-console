package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vacks/fluxzy-console/internal/auth"
	"github.com/vacks/fluxzy-console/internal/binary"
	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/retention"
	"github.com/vacks/fluxzy-console/internal/rotation"
	"github.com/vacks/fluxzy-console/internal/rules"
	"github.com/vacks/fluxzy-console/internal/runtimeconfig"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
	"github.com/vacks/fluxzy-console/internal/trustca"
	webui "github.com/vacks/fluxzy-console/web"
)

type Deps struct {
	Cfg           config.Config
	Store         *store.Store
	Auth          *auth.Service
	Capture       *capture.Manager
	Rules         *rules.Service
	CA            *trustca.Manager
	Retention     *retention.Manager
	Supervisor    *supervisor.Supervisor
	RuntimeConfig *runtimeconfig.Service
	Rotation      *rotation.Manager
	Resolver      *binary.Resolver
	Hub           *live.Hub
	Log           *slog.Logger
	Version       string
}

type Handlers struct {
	deps   Deps
	userMu sync.Mutex
}

func New(deps Deps) http.Handler {
	h := &Handlers{deps: deps}
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// Do not use middleware.RealIP: it trusts X-Forwarded-For from any peer,
	// which would let a direct client spoof the login rate-limit key and audit
	// IP. clientIP() applies forwarded headers only for trusted proxy peers.
	r.Use(h.recoverPanic)
	r.Use(h.securityHeaders)
	r.Use(h.logRequests)

	r.Handle("/metrics", h.metricsAuth(promhttp.Handler()))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", h.healthz)
		r.Get("/readyz", h.readyz)

		r.Get("/auth/status", h.authStatus)
		r.Post("/auth/login", h.login)
		r.Post("/auth/setup", h.setup)

		r.Group(func(r chi.Router) {
			r.Use(h.authenticate)
			r.Use(h.csrf)

			r.Get("/auth/me", h.me)
			r.Post("/auth/logout", h.logout)
			r.Post("/auth/password", h.changePassword)

			r.Get("/capture/status", h.captureStatus)
			r.Get("/capture/sessions", h.listSessions)
			r.Get("/capture/sessions/{id}", h.getSession)
			r.Get("/capture/sessions/{id}/stats", h.sessionStats)
			r.Get("/capture/sessions/{id}/aggregates", h.sessionAggregates)
			r.Get("/capture/sessions/{id}/top-hosts", h.sessionTopHosts)
			r.Get("/capture/sessions/{id}/exchanges", h.listExchanges)
			r.Get("/capture/sessions/{id}/exchanges/{eid}", h.getExchange)
			r.Get("/capture/sessions/{id}/exchanges/{eid}/body", h.getExchangeBody)
			r.Get("/capture/sessions/{id}/exchanges/{eid}/ws", h.getWebSocketMessages)
			r.With(h.requireOperator).Get("/capture/sessions/{id}/export", h.exportSession)

			r.With(h.requireOperator).Post("/capture/sessions/{id}/pin", h.pinSession)
			r.With(h.requireOperator).Delete("/capture/sessions/{id}", h.deleteSession)

			r.Get("/rules", h.rulesStatus)
			r.Get("/rules/schema", h.rulesSchema)
			r.Get("/rules/versions", h.rulesVersions)
			r.Get("/rules/versions/{id}", h.rulesVersion)
			r.With(h.requireOperator).Post("/rules/validate", h.rulesValidate)
			r.Post("/rules/render", h.rulesRender)
			r.Post("/rules/parse", h.rulesParse)
			r.With(h.requireAdmin).Post("/rules/save", h.rulesSave)
			r.With(h.requireAdmin).Post("/rules/apply", h.rulesApply)
			r.With(h.requireAdmin).Post("/rules/rollback", h.rulesRollback)

			r.With(h.requireAdmin).Get("/audit", h.listAudit)

			r.With(h.requireAdmin).Get("/users", h.listUsers)
			r.With(h.requireAdmin).Post("/users", h.createUser)
			r.With(h.requireAdmin).Patch("/users/{id}", h.updateUser)
			r.With(h.requireAdmin).Delete("/users/{id}", h.deleteUser)

			r.Get("/system/info", h.systemInfo)
			r.With(h.requireOperator).Get("/system/ca", h.downloadCA)
			r.With(h.requireOperator).Get("/system/ca/status", h.caStatus)
			r.With(h.requireAdmin).Post("/system/ca", h.caUpload)
			r.With(h.requireAdmin).Post("/system/ca/regenerate", h.caRegenerate)
			r.With(h.requireAdmin).Delete("/system/ca", h.caReset)

			r.Get("/system/fluxzy", h.runtimeStatus)
			r.With(h.requireOperator).Post("/system/fluxzy/start", h.runtimeStart)
			r.With(h.requireOperator).Post("/system/fluxzy/stop", h.runtimeStop)
			r.With(h.requireOperator).Post("/system/fluxzy/restart", h.runtimeRestart)
			r.With(h.requireOperator).Post("/system/fluxzy/rotate", h.runtimeRotate)
			r.With(h.requireOperator).Get("/system/fluxzy/logs", h.runtimeLogs)
			r.Get("/system/fluxzy/startup", h.runtimeStartup)
			r.With(h.requireAdmin).Put("/system/fluxzy/startup", h.runtimeStartupUpdate)
			r.With(h.requireAdmin).Post("/system/fluxzy/startup/apply", h.runtimeStartupApply)
			r.Get("/system/fluxzy/rotation", h.rotationSettings)
			r.With(h.requireAdmin).Put("/system/fluxzy/rotation", h.rotationUpdate)

			r.Get("/settings/retention", h.getRetentionSettings)
			r.With(h.requireAdmin).Put("/settings/retention", h.updateRetentionSettings)

			r.Get("/live", h.liveWS)
		})
	})

	spa := webui.Handler()
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") || req.URL.Path == "/metrics" {
			writeError(w, http.StatusNotFound, "not_found", "接口不存在", nil)
			return
		}
		spa.ServeHTTP(w, req)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "方法不允许", nil)
	})

	return r
}

func (h *Handlers) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; connect-src 'self' ws: wss:; font-src 'self' data:; "+
				"object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
