package rules

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ValidationResult struct {
	Valid      bool     `json:"valid"`
	Skipped    bool     `json:"skipped"`
	Errors     []string `json:"errors"`
	Warnings   []string `json:"warnings"`
	Output     string   `json:"output,omitempty"`
	DurationMs int64    `json:"durationMs"`
}

type PathResolver interface {
	Path(ctx context.Context) (string, error)
}

type CLI struct {
	resolver PathResolver
	log      *slog.Logger
	// sem bounds concurrent fluxzy subprocesses. Validation, CA export and
	// archive packing are CPU/memory heavy and must not be unbounded.
	sem chan struct{}
}

func NewCLI(resolver PathResolver, log *slog.Logger) *CLI {
	return &CLI{resolver: resolver, log: log, sem: make(chan struct{}, 2)}
}

func (c *CLI) acquire(ctx context.Context) (func(), error) {
	select {
	case c.sem <- struct{}{}:
		return func() { <-c.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *CLI) Available() bool {
	if c == nil || c.resolver == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.resolver.Path(ctx)
	return err == nil
}

// Validate performs a real startup with the candidate rules on an ephemeral
// loopback port. If fluxzy is still running after a short grace period the
// rules are considered valid; if it exits with a non-zero code the stderr is
// returned to the user.
func (c *CLI) Validate(ctx context.Context, yamlText string) ValidationResult {
	started := time.Now()
	result := ValidationResult{Errors: []string{}, Warnings: []string{}}

	structErrs, warnings := ValidateStructure([]byte(yamlText))
	result.Warnings = append(result.Warnings, warnings...)
	if len(structErrs) > 0 {
		result.Errors = append(result.Errors, structErrs...)
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

	if !c.Available() {
		result.Valid = true
		result.Skipped = true
		result.Warnings = append(result.Warnings, "未找到 fluxzy CLI，已跳过运行时规则校验")
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

	release, err := c.acquire(ctx)
	if err != nil {
		result.Errors = append(result.Errors, "等待 fluxzy 校验进程超时: "+err.Error())
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	defer release()

	tmp, err := os.MkdirTemp("", "fluxzy-console-validate-*")
	if err != nil {
		result.Errors = append(result.Errors, "创建校验临时目录失败: "+err.Error())
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	defer os.RemoveAll(tmp)

	ruleFile := filepath.Join(tmp, "rules.yaml")
	if err := os.WriteFile(ruleFile, []byte(yamlText), 0o600); err != nil {
		result.Errors = append(result.Errors, "写入临时规则失败: "+err.Error())
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	home := filepath.Join(tmp, "home")
	_ = os.MkdirAll(home, 0o700)

	path, err := c.resolver.Path(ctx)
	if err != nil {
		result.Errors = append(result.Errors, "解析 fluxzy CLI 失败: "+err.Error())
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path,
		"start",
		"-l", "127.0.0.1:0",
		"-r", ruleFile,
		"--no-cert-cache",
	)
	cmd.Env = mergeEnv(map[string]string{
		"HOME":            home,
		"TMPDIR":          tmp,
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
	})

	var output syncBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		result.Errors = append(result.Errors, "启动 fluxzy 校验失败: "+err.Error())
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	grace := 2500 * time.Millisecond
	var waitErr error
	select {
	case waitErr = <-done:
		// The process exited on its own. Non-zero means the rules were
		// rejected or startup failed.
	case <-time.After(grace):
		// Still running: rules parsed and the proxy is listening.
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case waitErr = <-done:
		case <-time.After(1500 * time.Millisecond):
			_ = cmd.Process.Kill()
			<-done
		}
		waitErr = nil
	}

	result.DurationMs = time.Since(started).Milliseconds()
	result.Output = truncate(strings.TrimSpace(output.String()), 8000)

	if waitErr != nil {
		result.Valid = false
		msg := waitErr.Error()
		out := result.Output
		if out != "" {
			msg = out
		}
		result.Errors = append(result.Errors, msg)
		if strings.Contains(out, "Error while reading rule file") ||
			strings.Contains(out, "Unable to read yaml") ||
			strings.Contains(out, "Unable to detect") {
			result.Errors = append(result.Errors, "fluxzy 拒绝了该规则文件")
		}
		return result
	}

	result.Valid = true
	if strings.Contains(result.Output, "Error while reading rule file") {
		result.Valid = false
		result.Errors = append(result.Errors, result.Output)
	}
	return result
}

// ExportCA writes fluxzy's default root CA to out.
func (c *CLI) ExportCA(ctx context.Context, out string) error {
	if c == nil || c.resolver == nil {
		return fmt.Errorf("fluxzy CLI 不可用")
	}
	path, err := c.resolver.Path(ctx)
	if err != nil {
		return fmt.Errorf("fluxzy CLI 不可用: %w", err)
	}
	release, err := c.acquire(ctx)
	if err != nil {
		return fmt.Errorf("等待 fluxzy 进程超时: %w", err)
	}
	defer release()
	cmd := exec.CommandContext(ctx, path, "cert", "export", out)
	cmd.Env = mergeEnv(nil)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(combined)))
	}
	return nil
}

// Pack exports a dump directory to har, fluxzy or saz using the bundled CLI.
func (c *CLI) Pack(ctx context.Context, inputDir, out, format string) error {
	if c == nil || c.resolver == nil {
		return fmt.Errorf("fluxzy CLI 不可用")
	}
	path, err := c.resolver.Path(ctx)
	if err != nil {
		return fmt.Errorf("fluxzy CLI 不可用: %w", err)
	}
	release, err := c.acquire(ctx)
	if err != nil {
		return fmt.Errorf("等待 fluxzy 进程超时: %w", err)
	}
	defer release()
	args := []string{"pack", inputDir, out}
	if format != "" {
		// fluxzy's System.CommandLine definition exposes the long alias
		// --format; the short alias is not accepted by the CLI.
		args = append(args, "--format", format)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	// fluxzy 1.39.x disables Brotli decoding by default because the bundled
	// .NET runtime is affected by CVE-2026-111497. Historical sessions can
	// contain Brotli bodies, so allow the CLI to decode them while packing.
	// The flag only affects this short-lived export subprocess.
	cmd.Env = mergeEnv(map[string]string{"FLUXZY_DISABLE_BROTLI": "0"})
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(combined)))
	}
	return nil
}

type syncBuffer struct {
	mu        sync.Mutex
	head      bytes.Buffer
	tail      bytes.Buffer
	truncated bool
}

const (
	syncHeadLimit = 64 << 10
	syncTailLimit = 64 << 10
)

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	if room := syncHeadLimit - b.head.Len(); room > 0 {
		if len(p) <= room {
			_, _ = b.head.Write(p)
			return original, nil
		}
		_, _ = b.head.Write(p[:room])
		p = p[room:]
	}
	b.truncated = true
	_, _ = b.tail.Write(p)
	if b.tail.Len() > syncTailLimit {
		raw := b.tail.Bytes()
		trimmed := append([]byte(nil), raw[len(raw)-syncTailLimit:]...)
		b.tail.Reset()
		_, _ = b.tail.Write(trimmed)
	}
	return original, nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return b.head.String()
	}
	return b.head.String() + "\n...[输出已截断]...\n" + b.tail.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n... (truncated %d bytes)", len(s)-n)
}

func mergeEnv(overrides map[string]string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if _, replaced := overrides[key]; replaced {
			continue
		}
		if strings.HasPrefix(key, "CONSOLE_") {
			continue
		}
		out = append(out, entry)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}
