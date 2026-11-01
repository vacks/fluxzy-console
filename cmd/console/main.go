package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vacks/fluxzy-console/internal/auth"
	"github.com/vacks/fluxzy-console/internal/binary"
	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/httpapi"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/retention"
	"github.com/vacks/fluxzy-console/internal/rotation"
	"github.com/vacks/fluxzy-console/internal/rules"
	"github.com/vacks/fluxzy-console/internal/runtimeconfig"
	"github.com/vacks/fluxzy-console/internal/sessiondir"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
	"github.com/vacks/fluxzy-console/internal/trustca"
	"github.com/vacks/fluxzy-console/internal/version"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	if cfg.BundledFluxzyRelease == "" {
		cfg.BundledFluxzyRelease = version.BundledFluxzyRelease
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	log.Info("starting fluxzy-console",
		"version", version.Version,
		"listen", cfg.Listen,
		"data", cfg.FluxzyDataDir,
		"config", cfg.FluxzyConfigDir,
		"console_dir", cfg.ConsoleDir,
		"proxy", cfg.FluxzyProxyAddr,
	)

	st, err := store.Open(filepath.Join(cfg.ConsoleDir, "console.db"))
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// Persistence callbacks must outlive signal cancellation: the final
	// stopped/exit state and session-end marker are written during shutdown.
	persistCtx, persistCancel := context.WithCancel(context.Background())
	defer persistCancel()

	hub := live.NewHub()
	agg := capture.NewAggregator(st, log)
	mgr := capture.NewManager(cfg, st, hub, agg, log)
	if err := mgr.Start(rootCtx); err != nil {
		log.Error("start capture manager", "error", err)
		os.Exit(1)
	}

	resolver := binary.New(cfg.BundledFluxzyDir, cfg.BundledFluxzyRelease)
	rtCfg, err := runtimeconfig.New(rootCtx, st, runtimeconfig.Defaults(cfg), log)
	if err != nil {
		log.Error("load fluxzy startup config", "error", err)
		os.Exit(1)
	}
	sessionDirs := sessiondir.New(filepath.Join(cfg.FluxzyDataDir, "sessions"))

	var rulesSvc *rules.Service
	var sup *supervisor.Supervisor
	sup = supervisor.New(supervisor.Options{
		Sessions:  sessionDirs,
		RulesFile: cfg.RulesFile,
		Binary:    resolver,
		Spec: func(context.Context) (supervisor.StartSpec, string, error) {
			spec, hash := rtCfg.AppliedSpecWithHash()
			return spec, hash, nil
		},
		StopGrace:    cfg.StopGrace,
		StartTimeout: cfg.StartTimeout,
		HomeDir:      filepath.Join(cfg.ConsoleDir, "fluxzy-home"),
		TmpDir:       filepath.Join(cfg.ConsoleDir, "tmp"),
		Log:          log,
		Hub:          hub,
		OnStatus: func(status supervisor.Status) {
			running := status.State == supervisor.StateStarting ||
				status.State == supervisor.StateRunning ||
				status.State == supervisor.StateStopping
			mgr.SetProcessRunning(running)
			if status.State == supervisor.StateRunning {
				mgr.SetProxyAddr(supervisor.ProbeAddr(status.ListenAddr))
				if status.ActiveSessionID != "" && status.SpecHash != "" && rtCfg.AppliedHash() != status.SpecHash {
					if err := rtCfg.MarkApplied(persistCtx, status.SpecHash, status.ActiveSessionID); err != nil {
						log.Warn("mark startup config applied", "error", err)
					}
				}
			}
			if err := saveRuntimeStatus(persistCtx, st, status); err != nil {
				log.Warn("save runtime status", "error", err)
			}
		},
		OnEvent: func(ev supervisor.Event) {
			if err := st.AppendSupervisorEvent(persistCtx, store.SupervisorEvent{
				Kind: ev.Kind, SessionID: ev.SessionID, PID: ev.PID,
				ExitCode: ev.ExitCode, Message: ev.Message,
			}); err != nil {
				log.Warn("save supervisor event", "error", err)
			}
		},
		OnSessionStarting: func(id, dir string) error {
			rulesHash := rules.FileHash(cfg.RulesFile)
			if rulesSvc != nil {
				rulesHash = rulesSvc.CurrentHash()
			}
			configHash := rtCfg.Hash()
			if sup != nil {
				if applied := sup.LastSpecHash(); applied != "" {
					configHash = applied
				}
			}
			if err := st.MarkSessionStarting(persistCtx, id, dir, time.Now().UnixMilli(), configHash, rulesHash); err != nil {
				return err
			}
			mgr.SetExpectedSession(id, dir)
			return nil
		},
		OnSessionStopped: func(id, reason string) {
			mgr.EndActiveSession(id, reason)
		},
	})
	sup.SetContext(rootCtx)

	caMgr, err := trustca.New(resolver, filepath.Join(cfg.ConsoleDir, "ca"), sup, log)
	if err != nil {
		log.Error("initialize CA manager", "error", err)
		os.Exit(1)
	}
	if err := caMgr.ApplyEnv(); err != nil {
		log.Warn("custom CA is unusable; falling back to embedded CA", "error", err)
		if quarantineErr := caMgr.Quarantine(); quarantineErr != nil {
			log.Warn("quarantine invalid CA", "error", quarantineErr)
		}
		_ = caMgr.ApplyEnv()
	}

	cli := rules.NewCLI(resolver, log)
	rulesSvc = rules.NewService(cfg.RulesFile, st, mgr, cli, sup, log)
	if err := rulesSvc.EnsureDefault(rootCtx); err != nil {
		log.Warn("ensure default rules file", "error", err)
	}

	go func() {
		if err := sup.Start(rootCtx); err != nil {
			log.Warn("start fluxzy", "error", err)
		}
	}()

	authSvc := auth.NewService(st)
	if required, err := authSvc.SetupRequired(rootCtx); err == nil && required {
		if _, err := authSvc.Setup(rootCtx, cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword); err != nil {
			log.Error("bootstrap admin", "error", err)
		} else {
			log.Warn("default administrator created; change the password after the first login",
				"username", cfg.BootstrapAdminUsername)
		}
	}

	go cleanupLoop(rootCtx, st, authSvc, log)

	retentionMgr := retention.New(cfg, st, mgr, log)
	go retentionMgr.RunLoop(rootCtx)

	rotationMgr := rotation.New(st, sup, mgr, retentionMgr, cfg.FluxzyDataDir, hub, log)
	go rotationMgr.RunLoop(rootCtx)

	handler := httpapi.New(httpapi.Deps{
		Cfg:           cfg,
		Store:         st,
		Auth:          authSvc,
		Capture:       mgr,
		Rules:         rulesSvc,
		CA:            caMgr,
		Retention:     retentionMgr,
		Supervisor:    sup,
		RuntimeConfig: rtCfg,
		Rotation:      rotationMgr,
		Resolver:      resolver,
		Hub:           hub,
		Log:           log,
		Version:       version.Version,
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Streaming handlers clear their write deadline explicitly.
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		log.Info("shutdown requested")
	case err := <-errCh:
		log.Error("http server", "error", err)
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		httpCtx, httpCancel := context.WithTimeout(shutdownCtx, 8*time.Second)
		_ = srv.Shutdown(httpCtx)
		httpCancel()
		if err := sup.Shutdown(shutdownCtx); err != nil {
			log.Warn("stop fluxzy", "error", err)
		}
		mgr.Stop()
		hub.Close()
		close(done)
	}()

	select {
	case <-done:
		log.Info("stopped")
	case <-shutdownCtx.Done():
		log.Warn("graceful shutdown timed out; force-killing fluxzy")
	}
	sup.ForceKill()
}

func saveRuntimeStatus(ctx context.Context, st *store.Store, status supervisor.Status) error {
	return st.SaveRuntimeStatus(ctx, store.RuntimeStatus{
		State:               string(status.State),
		PID:                 status.PID,
		ActiveSessionID:     status.ActiveSessionID,
		BinaryVersion:       status.BinaryVersion,
		BinaryRelease:       status.BinaryRelease,
		BinaryPath:          status.BinaryPath,
		StartedAt:           status.StartedAt,
		LastExitCode:        status.LastExitCode,
		LastError:           status.LastError,
		ConsecutiveFailures: status.ConsecutiveFailures,
	})
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func cleanupLoop(ctx context.Context, st *store.Store, authSvc *auth.Service, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := st.DeleteExpiredAuthSessions(ctx); err != nil {
				log.Warn("cleanup auth sessions", "error", err)
			}
			if authSvc != nil {
				authSvc.PruneTouch(time.Now().Add(-2 * time.Hour))
			}
			cutoff := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
			if err := st.PurgeSupervisorEvents(ctx, cutoff); err != nil {
				log.Warn("purge supervisor events", "error", err)
			}
			if err := st.PurgeAudit(ctx, cutoff); err != nil {
				log.Warn("purge audit log", "error", err)
			}
		}
	}
}
