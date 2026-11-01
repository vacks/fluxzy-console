package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vacks/fluxzy-console/internal/binary"
	"github.com/vacks/fluxzy-console/internal/live"
	"github.com/vacks/fluxzy-console/internal/sessiondir"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateBackoff  State = "backoff"
	StateFailed   State = "failed"
	StateDiskFull State = "disk_full"
)

var (
	ErrAlreadyRunning = errors.New("fluxzy 已在运行")
	ErrNotRunning     = errors.New("fluxzy 未运行")
)

type Status struct {
	State               State  `json:"state"`
	PID                 int    `json:"pid"`
	ActiveSessionID     string `json:"activeSessionId"`
	BinaryVersion       string `json:"binaryVersion"`
	BinaryRelease       string `json:"binaryRelease"`
	BinaryPath          string `json:"binaryPath"`
	BinarySource        string `json:"binarySource"`
	SpecHash            string `json:"specHash,omitempty"`
	ListenAddr          string `json:"listenAddr,omitempty"`
	StartedAt           int64  `json:"startedAt"`
	UptimeMs            int64  `json:"uptimeMs"`
	LastExitCode        *int   `json:"lastExitCode,omitempty"`
	LastError           string `json:"lastError"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	RestartCount        int    `json:"restartCount"`
	NextBackoffMs       int64  `json:"nextBackoffMs"`
	Intentional         bool   `json:"intentional"`
}

type Event struct {
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Message   string `json:"message"`
	At        int64  `json:"at"`
}

type BinaryResolver interface {
	Resolve(ctx context.Context) (binary.Info, error)
}

type Options struct {
	Sessions  *sessiondir.Manager
	RulesFile string
	Binary    BinaryResolver
	// Spec returns the startup spec and the runtime-config hash that produced
	// it. The hash is captured atomically with the spec so applied-state
	// bookkeeping cannot describe a config the process is not using.
	Spec func(ctx context.Context) (StartSpec, string, error)

	StopGrace    time.Duration
	StartTimeout time.Duration
	HomeDir      string
	TmpDir       string

	Log *slog.Logger
	Hub *live.Hub

	OnStatus          func(Status)
	OnEvent           func(Event)
	OnSessionStarting func(id, dir string) error
	OnSessionStopped  func(id, reason string)
}

type Supervisor struct {
	opts Options
	log  *slog.Logger
	logs *LogBuffer

	mu       sync.Mutex
	opMu     sync.Mutex
	ctx      context.Context
	state    State
	run      *run
	gen      uint64
	status   Status
	lastBin  binary.Info
	spec     StartSpec
	specHash string
	nextWait time.Duration
}

type run struct {
	pid        int
	cmd        *exec.Cmd
	sessionID  string
	sessionDir string
	generation uint64
	startedAt  time.Time
	stopReason string
	stopFlag   bool

	waitDone chan struct{}
	done     chan struct{}
	exitCode *int
}

func New(opts Options) *Supervisor {
	if opts.StopGrace <= 0 {
		opts.StopGrace = 10 * time.Second
	}
	if opts.StartTimeout <= 0 {
		opts.StartTimeout = 20 * time.Second
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Supervisor{
		opts:     opts,
		log:      opts.Log,
		logs:     NewLogBuffer(5000, os.Stdout, os.Stderr),
		state:    StateStopped,
		nextWait: time.Second,
	}
}

func (s *Supervisor) SetContext(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
}

func (s *Supervisor) Logs(since int64, tail int) []LogLine {
	return s.logs.Tail(since, tail)
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Supervisor) statusLocked() Status {
	st := s.status
	st.State = s.state
	if s.run != nil {
		st.PID = s.run.pid
		st.ActiveSessionID = s.run.sessionID
		if s.state == StateRunning || s.state == StateStarting {
			st.UptimeMs = time.Since(s.run.startedAt).Milliseconds()
		}
	} else {
		st.PID = 0
	}
	st.BinaryVersion = s.lastBin.Version
	st.BinaryRelease = s.lastBin.ReleaseTag
	st.BinaryPath = s.lastBin.Path
	st.BinarySource = string(s.lastBin.Source)
	st.SpecHash = s.specHash
	st.ListenAddr = s.spec.ListenAddr
	return st
}

// LastSpecHash returns the runtime-config hash captured for the most recent
// start. It is empty before the first start.
func (s *Supervisor) LastSpecHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.specHash
}

func (s *Supervisor) lockOp(ctx context.Context) bool {
	for {
		if s.opMu.TryLock() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (s *Supervisor) Start(ctx context.Context) error {
	if !s.lockOp(ctx) {
		return ctx.Err()
	}
	defer s.opMu.Unlock()
	return s.startLocked(ctx)
}

func (s *Supervisor) startLocked(ctx context.Context) error {
	return s.startSpecLocked(ctx, nil, "")
}

func (s *Supervisor) startSpecLocked(ctx context.Context, override *StartSpec, overrideHash string) error {
	s.mu.Lock()
	if s.ctx == nil {
		s.ctx = ctx
	}
	current := s.state
	s.mu.Unlock()
	if current == StateStarting || current == StateRunning || current == StateStopping {
		return ErrAlreadyRunning
	}

	bin, err := s.opts.Binary.Resolve(ctx)
	if err != nil {
		s.fail("resolve fluxzy CLI", err)
		return err
	}
	var spec StartSpec
	var specHash string
	if override != nil {
		spec, specHash = *override, overrideHash
	} else {
		spec, specHash, err = s.opts.Spec(ctx)
		if err != nil {
			s.fail("load startup config", err)
			return err
		}
	}
	if err := spec.Validate(); err != nil {
		s.fail("validate startup config", err)
		return err
	}
	s.mu.Lock()
	s.specHash = specHash
	s.spec = spec
	s.mu.Unlock()

	id, dir, err := s.opts.Sessions.Next(time.Now())
	if err != nil {
		s.fail("create session directory", err)
		return err
	}

	if s.opts.OnSessionStarting != nil {
		if err := s.opts.OnSessionStarting(id, dir); err != nil {
			s.fail("register session", err)
			return err
		}
	}

	cmd := exec.Command(bin.Path, spec.Args(s.opts.RulesFile, dir)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = "/"
	cmd.Stdout = s.logs.Stdout()
	cmd.Stderr = s.logs.Stderr()
	cmd.Env = mergeEnv(map[string]string{
		"HOME":                                  s.opts.HomeDir,
		"XDG_CACHE_HOME":                        s.opts.HomeDir + "/.cache",
		"XDG_CONFIG_HOME":                       s.opts.HomeDir + "/.config",
		"TMPDIR":                                s.opts.TmpDir,
		"DOTNET_SYSTEM_GLOBALIZATION_INVARIANT": "1",
		// The bundled CLI watches the rule file and reloads rules in-process.
		"FLUXZY_CONSOLE_RULE_HOT_RELOAD":  "1",
		"FLUXZY_CONSOLE_RULE_STATUS_FILE": filepath.Join(filepath.Dir(s.opts.RulesFile), ".rules-status.json"),
	})

	s.mu.Lock()
	s.gen++
	gen := s.gen
	r := &run{
		cmd:        cmd,
		sessionID:  id,
		sessionDir: dir,
		generation: gen,
		startedAt:  time.Now(),
		waitDone:   make(chan struct{}),
		done:       make(chan struct{}),
	}
	s.run = r
	s.state = StateStarting
	s.lastBin = bin
	s.spec = spec
	s.specHash = specHash
	s.status.StartedAt = r.startedAt.UnixMilli()
	s.status.LastError = ""
	s.status.Intentional = false
	s.mu.Unlock()
	s.emitStatus()
	s.emitEvent(Event{Kind: "starting", SessionID: id, Message: "starting fluxzy"})
	s.logs.append("console", "starting fluxzy: "+bin.Path+" (session "+id+")")
	logSeq := s.logs.LastSeq()

	if err := cmd.Start(); err != nil {
		s.logs.append("console", "failed to start fluxzy: "+err.Error())
		s.mu.Lock()
		s.run = nil
		s.state = StateFailed
		s.status.LastError = err.Error()
		s.mu.Unlock()
		s.emitStatus()
		s.emitEvent(Event{Kind: "exit", SessionID: id, Message: err.Error()})
		if s.opts.OnSessionStopped != nil {
			s.opts.OnSessionStopped(id, "start_failed")
		}
		return fmt.Errorf("start fluxzy: %w", err)
	}

	s.mu.Lock()
	if s.run == r {
		r.pid = cmd.Process.Pid
	}
	s.mu.Unlock()
	s.logs.append("console", fmt.Sprintf("fluxzy process started (pid %d)", cmd.Process.Pid))
	go s.waitLoop(r)

	if err := s.waitReady(ctx, r, spec, logSeq); err != nil {
		s.signal(r, syscall.SIGKILL)
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
		}
		s.mu.Lock()
		// handleExit owns the Backoff/Failed transition and may already have
		// scheduled a retry. Do not overwrite that decision here.
		if s.state != StateBackoff && s.state != StateFailed {
			s.state = StateFailed
		}
		if s.status.LastError == "" {
			s.status.LastError = err.Error()
		}
		s.mu.Unlock()
		s.emitStatus()
		s.emitEvent(Event{Kind: "exit", SessionID: id, Message: "readiness failed: " + err.Error()})
		return err
	}

	s.mu.Lock()
	// The process can exit immediately after printing its readiness banner.
	// handleExit then clears s.run and schedules (or refuses) a restart. Do
	// not publish StateRunning for a process that is already gone.
	if s.run != r || s.gen != r.generation {
		s.mu.Unlock()
		return fmt.Errorf("fluxzy exited immediately after readiness")
	}
	s.state = StateRunning
	s.status.StartedAt = r.startedAt.UnixMilli()
	s.status.LastError = ""
	s.status.NextBackoffMs = 0
	s.mu.Unlock()
	s.emitStatus()
	s.emitEvent(Event{Kind: "started", SessionID: id, PID: cmd.Process.Pid, Message: "fluxzy ready"})
	s.logs.append("console", "fluxzy is ready")
	s.scheduleStabilityReset(r)
	return nil
}

// scheduleStabilityReset clears the crash-loop counter only after the process
// has stayed alive for a full stability window. Resetting it at readiness lets
// a process that becomes ready and exits a second later restart forever.
func (s *Supervisor) scheduleStabilityReset(r *run) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		timer := time.NewTimer(60 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case <-timer.C:
		}
		s.mu.Lock()
		if s.run == r && s.state == StateRunning {
			s.status.ConsecutiveFailures = 0
			s.status.NextBackoffMs = 0
			s.nextWait = time.Second
		}
		s.mu.Unlock()
		s.emitStatus()
	}()
}

func (s *Supervisor) waitReady(ctx context.Context, r *run, spec StartSpec, logSeq int64) error {
	deadline := time.NewTimer(s.opts.StartTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.waitDone:
			return fmt.Errorf("fluxzy exited during startup: %s", s.lastLogText())
		case <-deadline.C:
			return fmt.Errorf("fluxzy did not report readiness on %s within %s", spec.ListenAddr, s.opts.StartTimeout)
		case <-ticker.C:
			if s.logs.ReadySince(logSeq) {
				time.Sleep(100 * time.Millisecond)
				select {
				case <-r.waitDone:
					return fmt.Errorf("fluxzy exited during startup: %s", s.lastLogText())
				default:
					return nil
				}
			}
		}
	}
}

func (s *Supervisor) waitLoop(r *run) {
	err := r.cmd.Wait()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	r.exitCode = &exitCode
	close(r.waitDone)
	s.handleExit(r, err, exitCode)
	close(r.done)
}

func (s *Supervisor) handleExit(r *run, err error, exitCode int) {
	s.logs.append("console", fmt.Sprintf("fluxzy exited (code %d)", exitCode))
	s.mu.Lock()
	if s.run != r {
		s.mu.Unlock()
		return
	}
	s.run = nil
	stoppedByUser := r.stopFlag || s.state == StateStopping
	s.status.LastExitCode = &exitCode
	uptime := time.Since(r.startedAt)
	if stoppedByUser {
		s.state = StateStopped
		s.status.LastError = ""
		s.status.NextBackoffMs = 0
		if uptime >= 60*time.Second {
			s.status.ConsecutiveFailures = 0
			s.nextWait = time.Second
		}
		s.mu.Unlock()
		s.emitStatus()
		s.emitEvent(Event{Kind: "stopped", SessionID: r.sessionID, ExitCode: &exitCode, Message: "fluxzy stopped"})
		if s.opts.OnSessionStopped != nil {
			s.opts.OnSessionStopped(r.sessionID, r.stopReason)
		}
		return
	}

	needsRestart := false
	restartDelay := s.nextWait
	if exitCode == 0 && s.spec.MaxCaptureCount > 0 {
		// fluxzy exits cleanly when -n is reached. Treat it as a controlled
		// rotation, not a crash. Keep the normal backoff for a subsequent crash.
		s.state = StateBackoff
		s.status.LastError = ""
		restartDelay = 0
		s.nextWait = time.Second
		needsRestart = true
	} else {
		if uptime >= 60*time.Second {
			s.status.ConsecutiveFailures = 0
			s.nextWait = time.Second
		}
		s.status.ConsecutiveFailures++
		s.status.RestartCount++
		s.status.LastError = errString(err, exitCode)
		if s.status.ConsecutiveFailures >= 5 {
			s.state = StateFailed
		} else {
			s.state = StateBackoff
			s.nextWait *= 2
			if s.nextWait > 60*time.Second {
				s.nextWait = 60 * time.Second
			}
			needsRestart = true
		}
	}
	s.status.NextBackoffMs = restartDelay.Milliseconds()
	s.mu.Unlock()

	s.emitStatus()
	s.emitEvent(Event{Kind: "exit", SessionID: r.sessionID, ExitCode: &exitCode, Message: s.Status().LastError})
	if s.opts.OnSessionStopped != nil {
		s.opts.OnSessionStopped(r.sessionID, "exit")
	}
	if needsRestart {
		s.scheduleRestart(r.generation, restartDelay)
	}
}

func (s *Supervisor) scheduleRestart(generation uint64, delay time.Duration) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		if delay > 0 {
			t := time.NewTimer(delay)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
		if !s.lockOp(ctx) {
			return
		}
		defer s.opMu.Unlock()
		s.mu.Lock()
		ok := s.gen == generation && s.state == StateBackoff && s.ctx != nil && s.ctx.Err() == nil
		s.mu.Unlock()
		if !ok {
			return
		}
		s.emitEvent(Event{Kind: "backoff", Message: "restarting fluxzy"})
		if err := s.startLocked(ctx); err != nil {
			s.log.Warn("fluxzy restart failed", "error", err)
		}
	}()
}

func (s *Supervisor) Stop(ctx context.Context, reason string) error {
	if !s.lockOp(ctx) {
		return ctx.Err()
	}
	defer s.opMu.Unlock()
	return s.stopLocked(ctx, reason)
}

func (s *Supervisor) stopLocked(ctx context.Context, reason string) error {
	s.mu.Lock()
	r := s.run
	if r == nil {
		s.state = StateStopped
		s.status.Intentional = true
		s.mu.Unlock()
		s.emitStatus()
		return nil
	}
	r.stopFlag = true
	r.stopReason = reason
	s.state = StateStopping
	s.status.Intentional = true
	s.mu.Unlock()
	s.emitStatus()
	s.emitEvent(Event{Kind: "stopping", SessionID: r.sessionID, Message: reason})
	s.logs.append("console", "stopping fluxzy: "+reason)

	s.signal(r, syscall.SIGTERM)
	timer := time.NewTimer(s.opts.StopGrace)
	defer timer.Stop()
	select {
	case <-r.done:
	case <-timer.C:
		s.signal(r, syscall.SIGKILL)
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			s.mu.Lock()
			s.state = StateFailed
			s.status.LastError = "fluxzy did not exit after SIGKILL"
			s.mu.Unlock()
			s.emitStatus()
			return fmt.Errorf("fluxzy did not exit after SIGKILL")
		}
	case <-ctx.Done():
		s.signal(r, syscall.SIGKILL)
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			s.mu.Lock()
			s.state = StateFailed
			s.status.LastError = "fluxzy did not exit after SIGKILL"
			s.mu.Unlock()
			s.emitStatus()
			return fmt.Errorf("fluxzy did not exit after SIGKILL")
		}
	}
	return nil
}

func (s *Supervisor) Restart(ctx context.Context, reason string) error {
	if !s.lockOp(ctx) {
		return ctx.Err()
	}
	defer s.opMu.Unlock()
	return s.restartLocked(ctx, reason, nil, "")
}

// RestartWithSpec applies an explicitly captured configuration. Automatic
// restarts use the applied configuration, never a saved-but-unapplied draft.
func (s *Supervisor) RestartWithSpec(ctx context.Context, reason string, spec StartSpec, hash string) error {
	if !s.lockOp(ctx) {
		return ctx.Err()
	}
	defer s.opMu.Unlock()
	return s.restartLocked(ctx, reason, &spec, hash)
}

func (s *Supervisor) restartLocked(ctx context.Context, reason string, spec *StartSpec, hash string) error {
	if err := s.stopLocked(ctx, reason); err != nil {
		return err
	}
	return s.startSpecLocked(ctx, spec, hash)
}

// Rotate only acts on a process that is currently meant to run. A queued
// rotation must not undo an explicit operator Stop.
func (s *Supervisor) Rotate(ctx context.Context, reason string) error {
	if !s.lockOp(ctx) {
		return ctx.Err()
	}
	defer s.opMu.Unlock()
	s.mu.Lock()
	running := s.state == StateRunning || s.state == StateStarting
	s.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	return s.restartLocked(ctx, "rotation:"+reason, nil, "")
}

func (s *Supervisor) Shutdown(ctx context.Context) error {
	return s.Stop(ctx, "shutdown")
}

// ForceKill is the final shutdown backstop. It does not wait for opMu, so it can
// stop a child even when another lifecycle operation is wedged.
func (s *Supervisor) ForceKill() {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return
	}
	s.signal(r, syscall.SIGKILL)
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
	}
}

// MarkDiskFull records the emergency stop reason after Stop has completed.
// Without this, the UI and the rotation loop cannot distinguish a deliberate
// disk-full stop from a user-requested stop.
func (s *Supervisor) MarkDiskFull() {
	s.mu.Lock()
	if s.run == nil {
		s.state = StateDiskFull
		s.status.LastError = "磁盘空间不足，fluxzy 已停止"
		s.status.NextBackoffMs = 0
	}
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Supervisor) signal(r *run, sig syscall.Signal) {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return
	}
	pid := r.cmd.Process.Pid
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = r.cmd.Process.Signal(sig)
	}
}

func (s *Supervisor) fail(prefix string, err error) {
	msg := prefix + ": " + err.Error()
	s.mu.Lock()
	s.state = StateFailed
	s.status.LastError = msg
	s.mu.Unlock()
	s.emitStatus()
	s.emitEvent(Event{Kind: "exit", Message: msg})
	s.log.Error("fluxzy supervisor", "error", msg)
}

func (s *Supervisor) emitStatus() {
	st := s.Status()
	if s.opts.OnStatus != nil {
		s.opts.OnStatus(st)
	}
	if s.opts.Hub != nil {
		s.opts.Hub.Publish(live.Event{Type: "runtime.status", Data: st})
	}
}

func (s *Supervisor) emitEvent(ev Event) {
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	if s.opts.OnEvent != nil {
		s.opts.OnEvent(ev)
	}
}

func (s *Supervisor) lastLogText() string {
	lines := s.logs.Tail(0, 8)
	out := ""
	for _, line := range lines {
		out += line.Stream + ": " + line.Text + "\n"
	}
	if out == "" {
		return "no output"
	}
	return out
}

func errString(err error, exitCode int) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("fluxzy exited with code %d", exitCode)
}

// mergeEnv applies overrides without leaving a duplicate inherited key. With
// duplicate keys, libc getenv returns the first match, so appending "HOME=..."
// to os.Environ() silently fails when HOME is already set.
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
		// Console-only secrets and configuration are not needed by fluxzy.
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
