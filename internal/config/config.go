package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Built-in defaults. The container has no runtime configuration environment
// variables: everything that can change after startup is edited in the web UI
// and persisted in SQLite. Paths and listen addresses are fixed by the image.
const (
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin"
)

// Config holds runtime configuration for the console. Values are built-in
// defaults; mutable fluxzy settings live in SQLite and are managed by
// internal/runtimeconfig.
type Config struct {
	Listen string

	ConsoleDir      string
	FluxzyDataDir   string
	FluxzyConfigDir string
	RulesFile       string

	// BundledFluxzyDir contains the fluxzy CLI shipped in the image.
	BundledFluxzyDir string
	// BundledFluxzyRelease is the four-part upstream release tag represented
	// by the bundled CLI.
	BundledFluxzyRelease string

	// FluxzyProxyAddr is the local probe address derived from the effective
	// listen address.
	FluxzyProxyAddr string
	// AdvertiseHost overrides the host shown to clients for the fluxzy proxy.
	// It is useful in containers where the host LAN address is not visible.
	AdvertiseHost string

	Startup FluxzyStartupDefaults

	BootstrapAdminUsername string
	BootstrapAdminPassword string

	HotRetention        time.Duration
	ColdRetention       time.Duration
	SamplePercent       int
	MaxBodyPreview      int64
	MaxLiveEventsPerSec int
	CookieSecure        bool
	TrustProxyHeaders   bool
	MetricsToken        string
	LogLevel            string
	AllowedOrigins      []string

	StopGrace    time.Duration
	StartTimeout time.Duration
}

// FluxzyStartupDefaults are the bootstrap values for the UI-managed startup
// configuration. They are only used when no row exists in fluxzy_runtime_config.
type FluxzyStartupDefaults struct {
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

func Load() (Config, error) {
	c := Config{
		Listen:                 ":8080",
		ConsoleDir:             "/var/lib/fluxzy-console",
		FluxzyDataDir:          "/data",
		FluxzyConfigDir:        "/config",
		BundledFluxzyDir:       "/opt/fluxzy/fork",
		BundledFluxzyRelease:   "",
		AdvertiseHost:          "",
		BootstrapAdminUsername: DefaultAdminUsername,
		BootstrapAdminPassword: DefaultAdminPassword,
		HotRetention:           24 * time.Hour,
		ColdRetention:          30 * 24 * time.Hour,
		SamplePercent:          5,
		MaxBodyPreview:         8 << 20,
		MaxLiveEventsPerSec:    20,
		CookieSecure:           false,
		TrustProxyHeaders:      false,
		MetricsToken:           "",
		LogLevel:               "info",
		StopGrace:              10 * time.Second,
		StartTimeout:           20 * time.Second,
		Startup: FluxzyStartupDefaults{
			ListenAddr:             "0.0.0.0:443",
			Mode:                   "ReverseSecure",
			ReversePort:            443,
			MaxUpstreamConnections: 16,
			RequestBuffer:          4096,
			MaxCaptureCount:        0,
			ServeH2:                false,
			ParseUA:                false,
			NoCertCache:            false,
			SkipSSLDecryption:      false,
		},
	}
	c.RulesFile = c.FluxzyConfigDir + "/rules.yaml"
	c.FluxzyProxyAddr = probeAddr(c.Startup.ListenAddr)

	for _, dir := range []string{
		c.ConsoleDir,
		c.FluxzyDataDir,
		c.FluxzyConfigDir,
		filepath.Join(c.ConsoleDir, "index"),
		filepath.Join(c.ConsoleDir, "exports"),
		filepath.Join(c.ConsoleDir, "logs"),
		filepath.Join(c.ConsoleDir, "tmp"),
		filepath.Join(c.ConsoleDir, "fluxzy-home"),
		filepath.Join(c.FluxzyDataDir, "sessions"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return c, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return c, nil
}

func probeAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1:443"
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}
