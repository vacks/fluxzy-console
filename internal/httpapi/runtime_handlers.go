package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vacks/fluxzy-console/internal/runtimeconfig"
	"github.com/vacks/fluxzy-console/internal/supervisor"
)

func (h *Handlers) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	if h.deps.Supervisor == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "fluxzy 进程监管未启动", nil)
		return
	}
	resp := map[string]any{
		"supervisor": h.deps.Supervisor.Status(),
		"capture":    h.deps.Capture.Status(),
	}
	if h.deps.RuntimeConfig != nil {
		resp["config"] = map[string]any{
			"settings":         h.deps.RuntimeConfig.Effective(),
			"defaults":         h.deps.RuntimeConfig.Defaults(),
			"hash":             h.deps.RuntimeConfig.Hash(),
			"appliedHash":      h.deps.RuntimeConfig.AppliedHash(),
			"appliedSessionId": h.deps.RuntimeConfig.AppliedSessionID(),
			"pending":          h.deps.RuntimeConfig.Pending(),
		}
	}
	if h.deps.Resolver != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		info, err := h.deps.Resolver.Resolve(ctx)
		cancel()
		if err == nil {
			resp["binary"] = info
		} else {
			resp["binaryError"] = err.Error()
		}
	}
	if h.deps.RuntimeConfig != nil {
		resp["proxyAddress"] = h.proxyAddress(r)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) runtimeStart(w http.ResponseWriter, r *http.Request) {
	h.runtimeAction(w, r, "start")
}

func (h *Handlers) runtimeStop(w http.ResponseWriter, r *http.Request) {
	h.runtimeAction(w, r, "stop")
}

func (h *Handlers) runtimeRestart(w http.ResponseWriter, r *http.Request) {
	h.runtimeAction(w, r, "restart")
}

func (h *Handlers) runtimeRotate(w http.ResponseWriter, r *http.Request) {
	h.runtimeAction(w, r, "rotate")
}

func (h *Handlers) runtimeAction(w http.ResponseWriter, r *http.Request, action string) {
	if h.deps.Supervisor == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "fluxzy 进程监管未启动", nil)
		return
	}
	// Lifecycle operations must survive the browser/reverse-proxy closing the
	// HTTP request. WithoutCancel keeps the operation running; the timeout is
	// still owned by the server so a hung fluxzy cannot block forever.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	var err error
	switch action {
	case "start":
		err = h.deps.Supervisor.Start(ctx)
	case "stop":
		err = h.deps.Supervisor.Stop(ctx, "manual")
	case "restart":
		err = h.deps.Supervisor.Restart(ctx, "manual")
	case "rotate":
		err = h.deps.Supervisor.Rotate(ctx, "manual")
	default:
		writeError(w, http.StatusBadRequest, "invalid_action", "未知操作", nil)
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if err == supervisor.ErrAlreadyRunning {
			status = http.StatusConflict
		}
		writeError(w, status, "runtime_action_failed", err.Error(), nil)
		return
	}
	h.audit(r, "fluxzy."+action, "", map[string]any{"sessionId": h.deps.Capture.ActiveSessionID()})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"action":  action,
		"status":  h.deps.Supervisor.Status(),
		"capture": h.deps.Capture.Status(),
	})
}

func (h *Handlers) runtimeLogs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Supervisor == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "fluxzy 进程监管未启动", nil)
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 2000 {
		tail = 200
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	lines := h.deps.Supervisor.Logs(since, tail)
	next := since
	if len(lines) > 0 {
		next = lines[len(lines)-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "nextSeq": next})
}

func (h *Handlers) runtimeStartup(w http.ResponseWriter, r *http.Request) {
	if h.deps.RuntimeConfig == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "启动配置服务未启动", nil)
		return
	}
	next := h.deps.RuntimeConfig.Effective()
	args := h.deps.RuntimeConfig.Spec().Args(
		h.deps.Cfg.RulesFile,
		filepath.Join(h.deps.Cfg.FluxzyDataDir, "sessions", "<new-session>"),
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":         next,
		"defaults":         h.deps.RuntimeConfig.Defaults(),
		"hash":             h.deps.RuntimeConfig.Hash(),
		"appliedHash":      h.deps.RuntimeConfig.AppliedHash(),
		"appliedSessionId": h.deps.RuntimeConfig.AppliedSessionID(),
		"pending":          h.deps.RuntimeConfig.Pending(),
		"applyMode":        "restart",
		"commandPreview":   append([]string{"fluxzy"}, args...),
		"derived": map[string]string{
			"rulesFile":    h.deps.Cfg.RulesFile,
			"sessionsRoot": filepath.Join(h.deps.Cfg.FluxzyDataDir, "sessions"),
		},
		"limits": map[string]any{
			"listenAddr":             "host:port, port 1-65535",
			"modes":                  []string{"Regular", "ReverseSecure", "ReversePlain"},
			"maxUpstreamConnections": map[string]int{"min": 1, "max": 1024},
			"requestBuffer":          map[string]int{"min": 16, "max": 524288},
			"maxCaptureCount":        map[string]int{"min": 0},
		},
		"warnings": []string{
			"修改监听地址/端口后，需要同步调整容器端口映射，否则外部无法访问。",
			"应用启动配置会重启 fluxzy、创建新会话并中断在途请求。",
		},
	})
}

func (h *Handlers) runtimeStartupUpdate(w http.ResponseWriter, r *http.Request) {
	if h.deps.RuntimeConfig == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "启动配置服务未启动", nil)
		return
	}
	var next runtimeconfig.Config
	if err := decodeJSON(w, r, &next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	author := ""
	if u := currentUser(r); u != nil {
		author = u.Username
	}
	if err := h.deps.RuntimeConfig.Update(r.Context(), next, author); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error(), nil)
		return
	}
	h.audit(r, "fluxzy.config.update", "", map[string]any{"hash": h.deps.RuntimeConfig.Hash()})
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":  h.deps.RuntimeConfig.Effective(),
		"hash":      h.deps.RuntimeConfig.Hash(),
		"pending":   h.deps.RuntimeConfig.Pending(),
		"applyMode": "restart",
		"message":   "启动配置已保存；应用后会重启 fluxzy 并创建新会话。",
	})
}

func (h *Handlers) runtimeStartupApply(w http.ResponseWriter, r *http.Request) {
	if h.deps.RuntimeConfig == nil || h.deps.Supervisor == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime_unavailable", "运行时服务未启动", nil)
		return
	}
	var req struct {
		ExpectedHash string `json:"expectedHash"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	targetSpec, hash := h.deps.RuntimeConfig.SpecWithHash()
	if req.ExpectedHash != "" && req.ExpectedHash != hash {
		writeError(w, http.StatusConflict, "config_changed", "启动配置已变化，请刷新后重试", nil)
		return
	}
	if !h.deps.RuntimeConfig.Pending() {
		if st := h.deps.Supervisor.Status(); st.State != supervisor.StateRunning {
			writeError(w, http.StatusConflict, "runtime_not_running",
				"当前启动配置已保存，但 fluxzy 未运行；请先启动 fluxzy", map[string]any{"state": st.State})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"applied": true, "message": "当前启动配置已经生效", "hash": hash,
			"sessionId": h.deps.Capture.ActiveSessionID(),
		})
		return
	}
	author := ""
	if u := currentUser(r); u != nil {
		author = u.Username
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()

	rollback := func(reason string) (bool, error) {
		restoreCtx, restoreCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
		defer restoreCancel()
		restored, restoredHash, restoreErr := h.deps.RuntimeConfig.RestoreApplied(restoreCtx, author)
		if restoreErr != nil {
			h.audit(r, "fluxzy.config.apply_failed", "", map[string]any{"error": reason, "rolledBack": false, "restoreError": restoreErr.Error()})
			return false, restoreErr
		}
		h.deps.Capture.SetProxyAddr(supervisor.ProbeAddr(restored.ListenAddr))
		if restartErr := h.deps.Supervisor.RestartWithSpec(restoreCtx, "config:restore:"+restoredHash, restored.Spec(), restoredHash); restartErr != nil {
			h.audit(r, "fluxzy.config.apply_failed", "", map[string]any{"error": reason, "rolledBack": false, "restoreError": restartErr.Error()})
			return false, restartErr
		}
		if reason != "" {
			h.audit(r, "fluxzy.config.apply_failed", "", map[string]any{"error": reason, "rolledBack": true})
		}
		return true, nil
	}

	if err := h.deps.Supervisor.RestartWithSpec(ctx, "config:"+hash, targetSpec, hash); err != nil {
		rolledBack, restoreErr := rollback(err.Error())
		writeError(w, http.StatusInternalServerError, "apply_failed", "启动配置应用失败: "+err.Error(), map[string]any{
			"rolledBack": rolledBack, "restoreError": errorString(restoreErr),
		})
		return
	}
	sessionID := h.deps.Capture.WaitForExpectedActive(ctx, 10*time.Second)
	if sessionID == "" {
		err := fmt.Errorf("fluxzy 已启动，但未确认新会话")
		rolledBack, restoreErr := rollback(err.Error())
		writeError(w, http.StatusInternalServerError, "apply_unconfirmed", err.Error(), map[string]any{
			"rolledBack": rolledBack, "restoreError": errorString(restoreErr),
		})
		return
	}
	if err := h.deps.RuntimeConfig.MarkApplied(ctx, hash, sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "保存应用状态失败", nil)
		return
	}
	h.deps.Capture.SetProxyAddr(supervisor.ProbeAddr(targetSpec.ListenAddr))
	h.audit(r, "fluxzy.config.apply", "", map[string]any{"hash": hash, "sessionId": sessionID})
	writeJSON(w, http.StatusOK, map[string]any{
		"applied": true, "hash": hash, "sessionId": sessionID,
		"settings": h.deps.RuntimeConfig.Effective(),
		"message":  "启动配置已应用；fluxzy 已重启并创建新会话。",
	})
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
