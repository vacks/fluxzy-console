package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vacks/fluxzy-console/internal/capture"
	"github.com/vacks/fluxzy-console/internal/store"
	"github.com/vacks/fluxzy-console/internal/supervisor"
)

func (h *Handlers) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Store.DB().PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "数据库不可用", nil)
		return
	}
	if _, err := os.Stat(h.deps.Cfg.FluxzyDataDir); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "数据目录不可用", nil)
		return
	}
	if h.deps.Resolver != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		_, err := h.deps.Resolver.Resolve(ctx)
		cancel()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "fluxzy CLI 不可用: "+err.Error(), nil)
			return
		}
	}
	if h.deps.Supervisor != nil {
		st := h.deps.Supervisor.Status()
		if st.State != supervisor.StateRunning {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "fluxzy 未运行: "+string(st.State),
				map[string]any{"state": st.State, "lastError": st.LastError})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type captureStatusResponse struct {
	capture.Status
	ActiveSession *sessionDTO `json:"activeSession,omitempty"`
	ProxyAddress  string      `json:"proxyAddress"`
}

func (h *Handlers) proxyAddress(r *http.Request) string {
	listen := h.deps.Cfg.Startup.ListenAddr
	if h.deps.RuntimeConfig != nil {
		listen = h.deps.RuntimeConfig.Effective().ListenAddr
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return supervisor.ProbeAddr(listen)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		// Bind-all: advertise a reachable address instead of loopback.
	default:
		return net.JoinHostPort(host, port)
	}
	if adv := strings.TrimSpace(h.deps.Cfg.AdvertiseHost); adv != "" {
		if ah, ap, splitErr := net.SplitHostPort(adv); splitErr == nil {
			return net.JoinHostPort(ah, ap)
		}
		return net.JoinHostPort(strings.Trim(adv, "[]"), port)
	}
	if r != nil {
		if rh := requestHostname(r.Host); rh != "" {
			if ip := net.ParseIP(rh); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				return net.JoinHostPort(ip.String(), port)
			}
		}
	}
	if ip := lanIPv4(); ip != "" {
		return net.JoinHostPort(ip, port)
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func requestHostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

// lanIPv4 returns the first usable non-loopback IPv4 address of this host.
func lanIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			if ip4.IsPrivate() {
				return ip4.String()
			}
			if fallback == "" {
				fallback = ip4.String()
			}
		}
	}
	return fallback
}

func (h *Handlers) captureStatus(w http.ResponseWriter, r *http.Request) {
	st := h.deps.Capture.Status()
	resp := captureStatusResponse{Status: st, ProxyAddress: h.proxyAddress(r)}
	if st.ActiveSessionID != "" {
		cs := st.ActiveSession
		if cs == nil {
			// Fallback for a session discovered before the in-memory snapshot
			// was initialized.
			var err error
			cs, err = h.deps.Store.GetCaptureSession(r.Context(), st.ActiveSessionID)
			if err != nil {
				cs = nil
			}
		}
		if cs != nil {
			dto := toSessionDTO(cs)
			dto.Active = true
			dto.Online = h.deps.Capture.Online()
			dto.HasRaw = dirExists(cs.Dir)
			dto.HasCold = fileExists(h.coldIndexPath(dto.ID))
			resp.ActiveSession = &dto
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type sessionDTO struct {
	store.CaptureSession
	Active  bool `json:"active"`
	Online  bool `json:"online"`
	HasRaw  bool `json:"hasRaw"`
	HasCold bool `json:"hasCold"`
}

func (h *Handlers) listSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	if pageSize <= 0 {
		// Keep accepting the old limit parameter for the overview card.
		pageSize, _ = strconv.Atoi(q.Get("limit"))
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 200 {
		pageSize = 200
	}
	activeID := h.deps.Capture.ActiveSessionID()
	online := h.deps.Capture.Online()
	statusFilter := q.Get("status")
	if statusFilter != "live" && statusFilter != "archived" && statusFilter != "sampled" {
		statusFilter = ""
	}
	sessions, total, err := h.deps.Store.ListCaptureSessionsPage(
		r.Context(), pageSize, (page-1)*pageSize, strings.TrimSpace(q.Get("search")), statusFilter, activeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取会话列表失败", nil)
		return
	}
	out := make([]sessionDTO, 0, len(sessions))
	for i := range sessions {
		dto := toSessionDTO(&sessions[i])
		dto.Active = dto.ID == activeID
		dto.Online = dto.Active && online
		dto.HasRaw = dirExists(dto.Dir)
		dto.HasCold = fileExists(h.coldIndexPath(dto.ID))
		out = append(out, dto)
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":           out,
		"total":           total,
		"page":            page,
		"pageSize":        pageSize,
		"totalPages":      totalPages,
		"activeSessionId": activeID,
		"online":          online,
	})
}

func toSessionDTO(cs *store.CaptureSession) sessionDTO {
	return sessionDTO{CaptureSession: *cs}
}

func (h *Handlers) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cs, err := h.deps.Store.GetCaptureSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		return
	}
	dto := toSessionDTO(cs)
	dto.Active = h.deps.Capture.ActiveSessionID() == id
	dto.Online = dto.Active && h.deps.Capture.Online()
	dto.HasRaw = dirExists(cs.Dir)
	dto.HasCold = fileExists(h.coldIndexPath(id))

	var meta json.RawMessage
	if cs.MetaJSON != "" && json.Valid([]byte(cs.MetaJSON)) {
		meta = json.RawMessage(cs.MetaJSON)
	}
	errorsCount, _ := h.deps.Store.CountIndexErrors(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"session":     dto,
		"meta":        meta,
		"indexErrors": errorsCount,
	})
}

func (h *Handlers) sessionStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == h.deps.Capture.ActiveSessionID() {
		writeJSON(w, http.StatusOK, h.deps.Capture.Status().Stats)
		return
	}
	// Prefer the trigger-maintained totals in the session index (exact and
	// O(1)). Falls back to minute aggregates for purged sessions whose index
	// is gone.
	var a store.Aggregate
	fromIndex := false
	if idx, cold, closeFn, idxErr := h.openSessionIndex(id); idxErr == nil {
		if !cold {
			if agg, aggErr := idx.AggregateStats(r.Context()); aggErr == nil {
				a = agg
				fromIndex = true
			}
		}
		closeFn()
	}
	if !fromIndex {
		var err error
		a, err = h.deps.Store.SumAggregates(r.Context(), id)
		if err != nil {
			// Fall back to the sampled cold index only when exact aggregate
			// rows have also been purged.
			if idx, _, closeFn, idxErr := h.openSessionIndex(id); idxErr == nil {
				if agg, aggErr := idx.AggregateStats(r.Context()); aggErr == nil {
					a = agg
					fromIndex = true
				}
				closeFn()
			}
			if !fromIndex {
				writeError(w, http.StatusInternalServerError, "internal_error", "读取统计失败", nil)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":   id,
		"total":       a.Count,
		"reqBytes":    a.ReqBytes,
		"respBytes":   a.RespBytes,
		"status2xx":   a.Status2xx,
		"status3xx":   a.Status3xx,
		"status4xx":   a.Status4xx,
		"status5xx":   a.Status5xx,
		"errors":      a.Errors,
		"rps":         0,
		"bytesPerSec": 0,
	})
}

func (h *Handlers) sessionAggregates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if since <= 0 {
		since = time.Now().Add(-24 * time.Hour).Unix()
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.deps.Store.ListAggregates(r.Context(), id, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取聚合数据失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) sessionTopHosts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx, _, closeFn, err := h.openSessionIndex(id)
	if err != nil {
		writeError(w, http.StatusGone, "session_expired", "该会话的索引已过期或被清理", nil)
		return
	}
	defer closeFn()
	items, err := idx.TopHosts(r.Context(), 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取域名统计失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type pinRequest struct {
	Pinned bool `json:"pinned"`
}

func (h *Handlers) pinSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req pinRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := h.deps.Store.SetCaptureSessionPinned(r.Context(), id, req.Pinned); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "更新会话失败", nil)
		}
		return
	}
	h.audit(r, "session.pin", id, map[string]any{"pinned": req.Pinned})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == h.deps.Capture.ActiveSessionID() {
		writeError(w, http.StatusConflict, "active_session", "不能删除当前正在写入的会话", nil)
		return
	}
	cs, err := h.deps.Store.GetCaptureSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		return
	}
	if !safeUnder(cs.Dir, filepath.Join(h.deps.Cfg.FluxzyDataDir, "sessions")) ||
		filepath.Base(filepath.Clean(cs.Dir)) != cs.ID {
		writeError(w, http.StatusBadRequest, "invalid_path", "会话路径不合法", nil)
		return
	}
	h.deps.Capture.RemoveSession(id)
	_ = os.RemoveAll(cs.Dir)
	removeIndexFiles(h.hotIndexPath(id))
	removeIndexFiles(h.coldIndexPath(id))
	if err := h.deps.Store.DeleteCaptureSession(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "删除会话记录失败", nil)
		return
	}
	h.audit(r, "session.delete", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) hotIndexPath(id string) string {
	return filepath.Join(h.deps.Cfg.ConsoleDir, "index", "hot-"+store.SanitizeSessionID(id)+".db")
}

func (h *Handlers) coldIndexPath(id string) string {
	return filepath.Join(h.deps.Cfg.ConsoleDir, "index", "cold-"+store.SanitizeSessionID(id)+".db")
}

func removeIndexFiles(p string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(p + suffix)
	}
}

func (h *Handlers) sessionDirSafe(cs *store.CaptureSession) bool {
	root := filepath.Join(h.deps.Cfg.FluxzyDataDir, "sessions")
	return cs != nil && safeUnder(cs.Dir, root) && filepath.Base(filepath.Clean(cs.Dir)) == cs.ID
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func safeUnder(path, root string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (h *Handlers) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	items, total, err := h.deps.Store.ListAuditPage(
		r.Context(), pageSize, (page-1)*pageSize,
		strings.TrimSpace(q.Get("search")), strings.TrimSpace(q.Get("action")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取日志失败", nil)
		return
	}
	actions, err := h.deps.Store.ListAuditActions(r.Context())
	if err != nil {
		h.deps.Log.Warn("list audit actions", "error", err)
		actions = []string{}
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
		"actions":    actions,
	})
}

func (h *Handlers) systemInfo(w http.ResponseWriter, r *http.Request) {
	activeID := h.deps.Capture.ActiveSessionID()
	sessionCount, _ := h.deps.Store.CountCaptureSessions(r.Context())
	idxErrs, _ := h.deps.Store.CountIndexErrors(r.Context(), activeID)

	hot := h.deps.Cfg.HotRetention
	cold := h.deps.Cfg.ColdRetention
	sample := h.deps.Cfg.SamplePercent
	retentionSettings := map[string]int{
		"hotHours":      int(hot / time.Hour),
		"coldDays":      int(cold / (24 * time.Hour)),
		"samplePercent": sample,
	}
	if h.deps.Retention != nil {
		s := h.deps.Retention.Settings()
		hot = s.HotDuration()
		cold = s.ColdDuration()
		sample = s.SamplePercent
		retentionSettings = map[string]int{
			"hotHours":      s.HotHours,
			"coldDays":      s.ColdDays,
			"samplePercent": s.SamplePercent,
		}
	}

	runtimeState := ""
	fluxzyVersion, fluxzyRelease, binarySource := "", "", ""
	if h.deps.Supervisor != nil {
		st := h.deps.Supervisor.Status()
		runtimeState = string(st.State)
		fluxzyVersion = st.BinaryVersion
		fluxzyRelease = st.BinaryRelease
		binarySource = st.BinarySource
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         h.deps.Version,
		"runtimeState":    runtimeState,
		"fluxzyVersion":   fluxzyVersion,
		"fluxzyRelease":   fluxzyRelease,
		"binarySource":    binarySource,
		"activeSessionId": activeID,
		"online":          h.deps.Capture.Online(),
		"proxyAddress":    h.proxyAddress(r),
		"dataDir":         h.deps.Cfg.FluxzyDataDir,
		"configDir":       h.deps.Cfg.FluxzyConfigDir,
		"consoleDir":      h.deps.Cfg.ConsoleDir,
		"rulesFile":       h.deps.Cfg.RulesFile,
		"sessionCount":    sessionCount,
		"indexErrors":     idxErrs,
		"cliAvailable":    h.deps.Rules.CLIAvailable(),
		"hotRetention":    hot.String(),
		"coldRetention":   cold.String(),
		"samplePercent":   sample,
		"retention":       retentionSettings,
		"now":             time.Now().UnixMilli(),
	})
}

func (h *Handlers) downloadCA(w http.ResponseWriter, r *http.Request) {
	clearWriteDeadline(w)
	if !h.deps.Rules.CLIAvailable() {
		writeError(w, http.StatusServiceUnavailable, "cli_unavailable", "未找到 fluxzy CLI，无法导出证书", nil)
		return
	}
	tmp, err := os.MkdirTemp(h.deps.Cfg.ConsoleDir, "ca-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建临时目录失败", nil)
		return
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "fluxzy-ca.cer")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.deps.Rules.ExportCA(ctx, out); err != nil {
		writeError(w, http.StatusInternalServerError, "ca_export_failed", "导出证书失败: "+err.Error(), nil)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="fluxzy-ca.cer"`)
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	http.ServeFile(w, r, out)
	h.audit(r, "system.ca_export", "ca", nil)
}
