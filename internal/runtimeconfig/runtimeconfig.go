package runtimeconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/vacks/fluxzy-console/internal/config"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
)

type Config struct {
	ListenAddr             string `json:"listenAddr"`
	Mode                   string `json:"mode"`
	ReversePort            int    `json:"reversePort"`
	MaxUpstreamConnections int    `json:"maxUpstreamConnections"`
	RequestBuffer          int    `json:"requestBuffer"`
	MaxCaptureCount        int64  `json:"maxCaptureCount"`
	ServeH2                bool   `json:"serveH2"`
	ParseUA                bool   `json:"parseUA"`
	NoCertCache            bool   `json:"noCertCache"`
	SkipSSLDecryption      bool   `json:"skipSSLDecryption"`
}

func Defaults(cfg config.Config) Config {
	return Config{
		ListenAddr:             cfg.Startup.ListenAddr,
		Mode:                   cfg.Startup.Mode,
		ReversePort:            cfg.Startup.ReversePort,
		MaxUpstreamConnections: cfg.Startup.MaxUpstreamConnections,
		RequestBuffer:          cfg.Startup.RequestBuffer,
		MaxCaptureCount:        cfg.Startup.MaxCaptureCount,
		ServeH2:                cfg.Startup.ServeH2,
		ParseUA:                cfg.Startup.ParseUA,
		NoCertCache:            cfg.Startup.NoCertCache,
		SkipSSLDecryption:      cfg.Startup.SkipSSLDecryption,
	}
}

func (c Config) Spec() supervisor.StartSpec {
	return supervisor.StartSpec{
		ListenAddr:             c.ListenAddr,
		Mode:                   c.Mode,
		ReversePort:            c.ReversePort,
		MaxUpstreamConnections: c.MaxUpstreamConnections,
		RequestBuffer:          c.RequestBuffer,
		MaxCaptureCount:        c.MaxCaptureCount,
		ServeH2:                c.ServeH2,
		ParseUA:                c.ParseUA,
		NoCertCache:            c.NoCertCache,
		SkipSSLDecryption:      c.SkipSSLDecryption,
	}
}

func (c Config) Validate() error { return c.Spec().Validate() }

func Hash(c Config) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type Service struct {
	store    *store.Store
	defaults Config
	log      *slog.Logger

	// opMu serializes current/applied transitions. mu only protects reads and
	// short field snapshots; it must not be held across database I/O.
	opMu             sync.Mutex
	mu               sync.RWMutex
	current          Config
	hash             string
	applied          Config
	appliedHash      string
	appliedSessionID string
}

func New(ctx context.Context, st *store.Store, defaults Config, log *slog.Logger) (*Service, error) {
	if err := defaults.Validate(); err != nil {
		return nil, fmt.Errorf("invalid fluxzy startup defaults: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Service{store: st, defaults: defaults, log: log}
	row, err := st.GetRuntimeConfig(ctx)
	if errors.Is(err, store.ErrNotFound) {
		s.current = defaults
		h, err := Hash(defaults)
		if err != nil {
			return nil, err
		}
		s.hash = h
		raw, _ := json.Marshal(defaults)
		if err := st.SaveRuntimeConfig(ctx, store.RuntimeConfig{
			ListenAddr:             defaults.ListenAddr,
			Mode:                   defaults.Mode,
			ReversePort:            defaults.ReversePort,
			MaxUpstreamConnections: defaults.MaxUpstreamConnections,
			RequestBuffer:          defaults.RequestBuffer,
			MaxCaptureCount:        defaults.MaxCaptureCount,
			ServeH2:                defaults.ServeH2,
			ParseUA:                defaults.ParseUA,
			NoCertCache:            defaults.NoCertCache,
			SkipSSLDecryption:      defaults.SkipSSLDecryption,
			ConfigHash:             h,
			ConfigJSON:             string(raw),
		}, "bootstrap"); err != nil {
			return nil, err
		}
		s.applied = defaults
		s.appliedHash = h
		if err := st.MarkRuntimeConfigApplied(ctx, h, ""); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := Config{
		ListenAddr:             row.ListenAddr,
		Mode:                   row.Mode,
		ReversePort:            row.ReversePort,
		MaxUpstreamConnections: row.MaxUpstreamConnections,
		RequestBuffer:          row.RequestBuffer,
		MaxCaptureCount:        row.MaxCaptureCount,
		ServeH2:                row.ServeH2,
		ParseUA:                row.ParseUA,
		NoCertCache:            row.NoCertCache,
		SkipSSLDecryption:      row.SkipSSLDecryption,
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("stored fluxzy startup config is invalid: %w", err)
	}
	h, err := Hash(cfg)
	if err != nil {
		return nil, err
	}
	s.current = cfg
	s.hash = h
	s.appliedHash = row.AppliedHash
	s.appliedSessionID = row.AppliedSessionID
	s.applied = defaults
	if row.AppliedHash != "" {
		if hist, histErr := st.GetRuntimeConfigHistoryByHash(ctx, row.AppliedHash); histErr == nil {
			var applied Config
			if json.Unmarshal([]byte(hist.ConfigJSON), &applied) == nil && applied.Validate() == nil {
				s.applied = applied
			}
		} else if row.AppliedHash == h {
			s.applied = cfg
		}
	}
	return s, nil
}

func (s *Service) Effective() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *Service) Spec() supervisor.StartSpec { return s.Effective().Spec() }

// SpecWithHash returns the exact spec and hash pair captured under one lock.
// The supervisor stores this hash so applied-state bookkeeping cannot drift
// when an admin saves a new config during startup.
func (s *Service) SpecWithHash() (supervisor.StartSpec, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Spec(), s.hash
}

func (s *Service) Applied() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.applied
}

// AppliedSpecWithHash returns the configuration the supervisor should use for
// ordinary starts and automatic restarts. Saved-but-unapplied changes are not
// picked up until an explicit apply supplies its own spec.
func (s *Service) AppliedSpecWithHash() (supervisor.StartSpec, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.appliedHash
	if h == "" {
		h, _ = Hash(s.applied)
	}
	return s.applied.Spec(), h
}

func (s *Service) Defaults() Config { return s.defaults }

func (s *Service) Hash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hash
}

func (s *Service) AppliedHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.appliedHash
}

func (s *Service) AppliedSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.appliedSessionID
}

func (s *Service) Pending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hash != s.appliedHash
}

func (s *Service) Update(ctx context.Context, next Config, author string) error {
	if err := next.Validate(); err != nil {
		return err
	}
	h, err := Hash(next)
	if err != nil {
		return err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.updateLocked(ctx, next, h, author)
}

func (s *Service) updateLocked(ctx context.Context, next Config, h, author string) error {
	raw, _ := json.Marshal(next)
	if err := s.store.SaveRuntimeConfig(ctx, store.RuntimeConfig{
		ListenAddr:             next.ListenAddr,
		Mode:                   next.Mode,
		ReversePort:            next.ReversePort,
		MaxUpstreamConnections: next.MaxUpstreamConnections,
		RequestBuffer:          next.RequestBuffer,
		MaxCaptureCount:        next.MaxCaptureCount,
		ServeH2:                next.ServeH2,
		ParseUA:                next.ParseUA,
		NoCertCache:            next.NoCertCache,
		SkipSSLDecryption:      next.SkipSSLDecryption,
		ConfigHash:             h,
		ConfigJSON:             string(raw),
	}, author); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = next
	s.hash = h
	s.mu.Unlock()
	return nil
}

// RestoreApplied writes the last applied configuration back as current. It is
// used when a candidate startup configuration fails to start.
func (s *Service) RestoreApplied(ctx context.Context, author string) (Config, string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	appliedHash := s.AppliedHash()
	if appliedHash == "" {
		h, err := Hash(s.defaults)
		if err != nil {
			return Config{}, "", err
		}
		if err := s.updateLocked(ctx, s.defaults, h, author); err != nil {
			return Config{}, "", err
		}
		return s.defaults, h, nil
	}
	row, err := s.store.GetRuntimeConfigHistoryByHash(ctx, appliedHash)
	if err != nil {
		return Config{}, "", err
	}
	var cfg Config
	if err := json.Unmarshal([]byte(row.ConfigJSON), &cfg); err != nil {
		return Config{}, "", err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, "", err
	}
	h, err := Hash(cfg)
	if err != nil {
		return Config{}, "", err
	}
	if err := s.updateLocked(ctx, cfg, h, author); err != nil {
		return Config{}, "", err
	}
	return cfg, h, nil
}

func (s *Service) MarkApplied(ctx context.Context, hash, sessionID string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	applied := s.applied
	if row, err := s.store.GetRuntimeConfigHistoryByHash(ctx, hash); err == nil {
		var cfg Config
		if json.Unmarshal([]byte(row.ConfigJSON), &cfg) == nil && cfg.Validate() == nil {
			applied = cfg
		}
	} else {
		s.mu.RLock()
		if hash == s.hash {
			applied = s.current
		}
		s.mu.RUnlock()
	}
	if err := s.store.MarkRuntimeConfigApplied(ctx, hash, sessionID); err != nil {
		return err
	}
	s.mu.Lock()
	s.applied = applied
	s.appliedHash = hash
	s.appliedSessionID = sessionID
	s.mu.Unlock()
	return nil
}

func (s *Service) History(ctx context.Context, limit int) ([]store.RuntimeConfigHistory, error) {
	return s.store.ListRuntimeConfigHistory(ctx, limit)
}
