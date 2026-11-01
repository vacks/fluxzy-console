package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vacks/fluxzy-console/internal/fluxzy/archive"
	"github.com/vacks/fluxzy-console/internal/store"
)

func (h *Handlers) listExchanges(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	status, _ := strconv.Atoi(q.Get("status"))

	filter := store.ExchangeFilter{
		Limit:       limit,
		BeforeID:    before,
		AfterID:     after,
		Query:       strings.TrimSpace(q.Get("q")),
		Method:      strings.TrimSpace(q.Get("method")),
		Status:      status,
		StatusCls:   strings.TrimSpace(q.Get("statusClass")),
		Host:        strings.TrimSpace(q.Get("host")),
		PendingOnly: q.Get("pending") == "1" || q.Get("pending") == "true",
		ErrorsOnly:  q.Get("errors") == "1" || q.Get("errors") == "true",
	}

	idx, cold, closeFn, err := h.openSessionIndex(id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		code := "internal_error"
		msg := "读取索引失败"
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, store.ErrNotFound) {
			statusCode = http.StatusGone
			code = "session_expired"
			msg = "该会话的索引已过期或被清理"
		}
		writeError(w, statusCode, code, msg, nil)
		return
	}
	defer closeFn()

	items, next, err := idx.Query(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "查询流量失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"nextCursor": next,
		"cold":       cold,
		"sampled":    cold,
		"count":      len(items),
	})
}

type exchangeDetailDTO struct {
	Exchange          store.ExchangeRow `json:"exchange"`
	Request           requestDTO        `json:"request"`
	Response          *responseDTO      `json:"response,omitempty"`
	Metrics           metricsDTO        `json:"metrics"`
	Tags              []string          `json:"tags"`
	Agent             *agentDTO         `json:"agent,omitempty"`
	ClientErrors      []clientErrorDTO  `json:"clientErrors"`
	ProcessInfo       *processInfoDTO   `json:"processInfo,omitempty"`
	Connection        *connectionDTO    `json:"connection,omitempty"`
	WebSocketMessages []wsMessageDTO    `json:"webSocketMessages,omitempty"`
	RequestTrailers   []headerDTO       `json:"requestTrailers,omitempty"`
	ResponseTrailers  []headerDTO       `json:"responseTrailers,omitempty"`
	Bodies            bodyInfoDTO       `json:"bodies"`
	Expired           bool              `json:"expired"`
}

type requestDTO struct {
	Method    string      `json:"method"`
	Scheme    string      `json:"scheme"`
	Path      string      `json:"path"`
	Authority string      `json:"authority"`
	URL       string      `json:"url"`
	Headers   []headerDTO `json:"headers"`
}

type responseDTO struct {
	StatusCode int         `json:"statusCode"`
	Headers    []headerDTO `json:"headers"`
}

type headerDTO struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Forwarded bool   `json:"forwarded,omitempty"`
}

type metricsDTO struct {
	StartedAt            string            `json:"startedAt"`
	DurationMs           int64             `json:"durationMs"`
	TotalSent            int64             `json:"totalSent"`
	TotalReceived        int64             `json:"totalReceived"`
	RequestHeaderLength  int               `json:"requestHeaderLength"`
	ResponseHeaderLength int               `json:"responseHeaderLength"`
	ReusingConnection    bool              `json:"reusingConnection"`
	DownStreamClientAddr string            `json:"downStreamClientAddress"`
	DownStreamClientPort int               `json:"downStreamClientPort"`
	DownStreamLocalAddr  string            `json:"downStreamLocalAddress"`
	DownStreamLocalPort  int               `json:"downStreamLocalPort"`
	Times                map[string]string `json:"times"`
}

type agentDTO struct {
	ID           int    `json:"id"`
	FriendlyName string `json:"friendlyName"`
}

type clientErrorDTO struct {
	ErrorCode        int    `json:"errorCode"`
	Message          string `json:"message"`
	ExceptionMessage string `json:"exceptionMessage,omitempty"`
	NetworkErrorCode string `json:"networkErrorCode,omitempty"`
}

type processInfoDTO struct {
	ProcessID        int    `json:"processId"`
	ProcessPath      string `json:"processPath,omitempty"`
	ProcessArguments string `json:"processArguments,omitempty"`
}

type wsMessageDTO struct {
	ID            int    `json:"id"`
	Direction     int    `json:"direction"`
	OpCode        int    `json:"opCode"`
	Length        int64  `json:"length"`
	WrittenLength int64  `json:"writtenLength"`
	Data          string `json:"data,omitempty"`
	MessageStart  string `json:"messageStart,omitempty"`
	MessageEnd    string `json:"messageEnd,omitempty"`
	FrameCount    int    `json:"frameCount"`
}

type connectionDTO struct {
	ID               int    `json:"id"`
	HTTPVersion      string `json:"httpVersion"`
	HostName         string `json:"hostName"`
	Port             int    `json:"port"`
	Secure           bool   `json:"secure"`
	RequestProcessed int    `json:"requestProcessed"`
	LocalPort        int    `json:"localPort"`
	LocalAddress     string `json:"localAddress"`
	RemoteAddress    string `json:"remoteAddress"`
	DNSStart         string `json:"dnsStart,omitempty"`
	DNSEnd           string `json:"dnsEnd,omitempty"`
	TcpOpening       string `json:"tcpOpening,omitempty"`
	TcpOpened        string `json:"tcpOpened,omitempty"`
	SslStart         string `json:"sslStart,omitempty"`
	SslEnd           string `json:"sslEnd,omitempty"`
}

type bodyInfoDTO struct {
	RequestExists  bool  `json:"requestExists"`
	ResponseExists bool  `json:"responseExists"`
	RequestSize    int64 `json:"requestSize"`
	ResponseSize   int64 `json:"responseSize"`
}

func (h *Handlers) getExchange(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	eid, err := strconv.ParseInt(chi.URLParam(r, "eid"), 10, 64)
	if err != nil || eid <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "无效的 exchange id", nil)
		return
	}
	idx, cold, closeFn, err := h.openSessionIndex(sessionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusGone, "session_expired", "该会话的索引已过期或被清理", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取会话索引失败", nil)
		}
		return
	}
	defer closeFn()

	row, err := idx.Get(r.Context(), eid)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "请求不存在", nil)
		return
	}

	detail := exchangeDetailDTO{
		Exchange:     *row,
		Request:      requestDTO{Headers: []headerDTO{}},
		Response:     &responseDTO{Headers: []headerDTO{}},
		Tags:         row.Tags,
		ClientErrors: []clientErrorDTO{},
		Expired:      cold,
		Metrics: metricsDTO{
			StartedAt:  time.UnixMilli(row.StartedAt).Format(time.RFC3339Nano),
			DurationMs: row.DurationMs,
			TotalSent:  row.ReqSize, TotalReceived: row.RespSize,
			Times: map[string]string{},
		},
	}

	cs, err := h.deps.Store.GetCaptureSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取会话失败", nil)
		}
		return
	}
	if !h.sessionDirSafe(cs) {
		writeError(w, http.StatusBadRequest, "invalid_path", "会话路径不合法", nil)
		return
	}

	if !cold {
		if ex, err := readExchangeMPack(cs.Dir, eid); err == nil {
			fillDetail(&detail, ex)
			if con, err := readConnectionMPack(cs.Dir, ex.ConnectionID); err == nil {
				detail.Connection = toConnectionDTO(con)
			}
		}
	}

	detail.Bodies = bodyInfo(cs.Dir, eid)
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handlers) getExchangeBody(w http.ResponseWriter, r *http.Request) {
	clearWriteDeadline(w)
	sessionID := chi.URLParam(r, "id")
	eid, err := strconv.ParseInt(chi.URLParam(r, "eid"), 10, 64)
	if err != nil || eid <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "无效的 exchange id", nil)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "request" && kind != "response" {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind 必须是 request 或 response", nil)
		return
	}
	cs, err := h.deps.Store.GetCaptureSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取会话失败", nil)
		}
		return
	}
	if !h.sessionDirSafe(cs) {
		writeError(w, http.StatusBadRequest, "invalid_path", "会话路径不合法", nil)
		return
	}
	if !dirExists(cs.Dir) {
		writeError(w, http.StatusGone, "session_expired", "原始数据已过期，仅保留采样索引", nil)
		return
	}
	prefix := "req"
	if kind == "response" {
		prefix = "res"
	}
	path := filepath.Join(cs.Dir, "contents", fmt.Sprintf("%s-%d.data", prefix, eid))
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not_found", "该请求没有 body", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 body 失败", nil)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取 body 信息失败", nil)
		return
	}

	contentType, contentEncoding := h.detectBodyHeaders(cs.Dir, eid, kind)
	baseType, charset := splitContentType(contentType)
	if baseType == "" {
		baseType = sniffContentType(f)
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "raw"
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s-%d.data"`, prefix, eid))

	if format == "text" {
		if !isTextualContentType(baseType) {
			writeError(w, http.StatusUnsupportedMediaType, "binary_body", "二进制内容，请使用 raw 下载", nil)
			return
		}
		limit := h.deps.Cfg.MaxBodyPreview
		if limit <= 0 {
			limit = 8 << 20
		}
		raw, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取 body 失败", nil)
			return
		}
		truncated := int64(len(raw)) > limit || int64(len(raw)) < info.Size()
		if int64(len(raw)) > limit {
			raw = raw[:limit]
		}
		data, decoded := decodeBodyPreview(raw, contentEncoding, charset, limit)
		if int64(len(data)) > limit {
			data = data[:limit]
			truncated = true
		}
		if truncated {
			data = append(data, []byte(fmt.Sprintf("\n\n... 已截断，完整大小 %d 字节", info.Size()))...)
		}
		if contentEncoding != "" {
			w.Header().Set("X-Body-Content-Encoding", contentEncoding)
		}
		w.Header().Set("X-Body-Decoded", strconv.FormatBool(decoded))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	downloadContentType := baseType
	if downloadContentType == "" {
		downloadContentType = "application/octet-stream"
	}
	if baseType == "" {
		if ct := sniffContentType(f); ct != "" {
			downloadContentType = ct
		}
	}
	if strings.HasPrefix(downloadContentType, "text/html") || strings.HasPrefix(downloadContentType, "application/xhtml") {
		// Never render captured HTML in the console origin.
		downloadContentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", downloadContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func (h *Handlers) getWebSocketMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	eid, err := strconv.ParseInt(chi.URLParam(r, "eid"), 10, 64)
	if err != nil || eid <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_id", "无效的 exchange id", nil)
		return
	}
	cs, err := h.deps.Store.GetCaptureSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取会话失败", nil)
		}
		return
	}
	if !h.sessionDirSafe(cs) {
		writeError(w, http.StatusBadRequest, "invalid_path", "会话路径不合法", nil)
		return
	}
	if !dirExists(cs.Dir) {
		writeError(w, http.StatusGone, "session_expired", "原始数据已过期", nil)
		return
	}
	ex, err := readExchangeMPack(cs.Dir, eid)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "无法读取该请求", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "解码请求失败", nil)
		}
		return
	}
	const maxMessages = 500
	const maxMessageData = 64 << 10
	out := make([]wsMessageDTO, 0, min(len(ex.WebSocketMessages), maxMessages))
	truncated := false
	for _, m := range ex.WebSocketMessages {
		if len(out) >= maxMessages {
			truncated = true
			break
		}
		dto := wsMessageDTO{
			ID: m.ID, Direction: m.Direction, OpCode: m.OpCode,
			Length: m.Length, WrittenLength: m.WrittenLength, FrameCount: m.FrameCount,
			MessageStart: formatTime(m.MessageStart), MessageEnd: formatTime(m.MessageEnd),
		}
		if m.DataString != "" {
			dto.Data = m.DataString
		} else if len(m.Data) > 0 {
			dto.Data = string(m.Data)
		}
		if len(dto.Data) > maxMessageData {
			dto.Data = dto.Data[:maxMessageData]
			truncated = true
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "truncated": truncated})
}

func (h *Handlers) exportSession(w http.ResponseWriter, r *http.Request) {
	clearWriteDeadline(w)
	sessionID := chi.URLParam(r, "id")
	cs, err := h.deps.Store.GetCaptureSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "会话不存在", nil)
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "读取会话失败", nil)
		}
		return
	}
	if !h.sessionDirSafe(cs) {
		writeError(w, http.StatusBadRequest, "invalid_path", "会话路径不合法", nil)
		return
	}
	if !dirExists(cs.Dir) {
		writeError(w, http.StatusGone, "session_expired", "原始数据已过期，无法导出", nil)
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "har"
	}
	cliFormat := format
	ext := format
	switch format {
	case "har":
		cliFormat = "har"
	case "fxzy", "fluxzy":
		cliFormat = "fluxzy"
		ext = "fxzy"
	case "saz":
		cliFormat = "saz"
	default:
		writeError(w, http.StatusBadRequest, "invalid_format", "format 必须是 har、fxzy 或 saz", nil)
		return
	}
	if !h.deps.Rules.CLIAvailable() {
		writeError(w, http.StatusServiceUnavailable, "cli_unavailable", "未找到 fluxzy CLI，无法导出", nil)
		return
	}

	exportDir := filepath.Join(h.deps.Cfg.ConsoleDir, "exports")
	if err := os.MkdirAll(exportDir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建导出目录失败", nil)
		return
	}
	tmpDir, err := os.MkdirTemp(exportDir, "export-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建导出目录失败", nil)
		return
	}
	defer os.RemoveAll(tmpDir)
	out := filepath.Join(tmpDir, fmt.Sprintf("fluxzy-%s.%s", store.SanitizeSessionID(sessionID), ext))

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := h.deps.Rules.Pack(ctx, cs.Dir, out, cliFormat); err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", "导出失败: "+err.Error(), nil)
		return
	}
	h.audit(r, "session.export", sessionID, map[string]any{"format": format})
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, store.SanitizeSessionID(sessionID), ext))
	http.ServeFile(w, r, out)
}

func (h *Handlers) openSessionIndex(id string) (*store.IndexStore, bool, func(), error) {
	// AcquireSession pins the cached session for the whole request; idle
	// sweeps cannot close its SQLite pool mid-query.
	if sess, release, ok := h.deps.Capture.AcquireSession(id); ok && sess.Index != nil {
		return sess.Index, false, release, nil
	}
	cs, csErr := h.deps.Store.GetCaptureSession(context.Background(), id)
	expired := csErr == nil && (cs.Status == "expired" || cs.Status == "purged")
	// Fallback for sessions whose DB row has been expired/removed but whose
	// index file still exists. Never revive a hot index for an expired row.
	if !expired {
		if hot := h.hotIndexPath(id); fileExists(hot) {
			idx, err := store.OpenIndexReadOnly(hot)
			if err != nil {
				return nil, false, func() {}, err
			}
			return idx, false, func() { _ = idx.Close() }, nil
		}
	}
	if cold := h.coldIndexPath(id); fileExists(cold) {
		idx, err := store.OpenIndexReadOnly(cold)
		if err != nil {
			return nil, true, func() {}, err
		}
		return idx, true, func() { _ = idx.Close() }, nil
	}
	return nil, false, func() {}, os.ErrNotExist
}

const maxArchiveFileBytes = 64 << 20

func readArchiveFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxArchiveFileBytes {
		return nil, fmt.Errorf("archive file is too large: %d bytes", info.Size())
	}
	return os.ReadFile(path)
}

func readExchangeMPack(dir string, id int64) (*archive.Exchange, error) {
	bucket := id / 100 * 100
	path := filepath.Join(dir, "exchanges", fmt.Sprintf("%d-%d", bucket, bucket+100), fmt.Sprintf("ex-%d.mpack", id))
	data, err := readArchiveFile(path)
	if err != nil {
		return nil, err
	}
	return archive.DecodeExchange(data)
}

func readConnectionMPack(dir string, id int) (*archive.Connection, error) {
	bucket := int64(id) / 100 * 100
	path := filepath.Join(dir, "connections", fmt.Sprintf("%d-%d", bucket, bucket+100), fmt.Sprintf("con-%d.mpack", id))
	data, err := readArchiveFile(path)
	if err != nil {
		return nil, err
	}
	return archive.DecodeConnection(data)
}

func fillDetail(d *exchangeDetailDTO, ex *archive.Exchange) {
	d.Request = requestDTO{
		Method: ex.Request.Method, Scheme: ex.Request.Scheme, Path: ex.Request.Path,
		Authority: ex.Request.Authority, URL: ex.FullURL(),
		Headers: toHeaderDTOs(ex.Request.Headers),
	}
	if ex.Response != nil {
		d.Response = &responseDTO{StatusCode: ex.Response.StatusCode, Headers: toHeaderDTOs(ex.Response.Headers)}
	}
	d.Tags = ex.Tags
	d.ClientErrors = make([]clientErrorDTO, 0, len(ex.ClientErrors))
	for _, e := range ex.ClientErrors {
		d.ClientErrors = append(d.ClientErrors, clientErrorDTO{
			ErrorCode: e.ErrorCode, Message: e.Message,
			ExceptionMessage: e.ExceptionMessage, NetworkErrorCode: e.NetworkErrorCode,
		})
	}
	if ex.Agent != nil {
		d.Agent = &agentDTO{ID: ex.Agent.ID, FriendlyName: ex.Agent.FriendlyName}
	}
	if ex.ProcessInfo != nil {
		d.ProcessInfo = &processInfoDTO{
			ProcessID: ex.ProcessInfo.ProcessID, ProcessPath: ex.ProcessInfo.ProcessPath,
			ProcessArguments: ex.ProcessInfo.ProcessArguments,
		}
	}
	d.RequestTrailers = toHeaderDTOs(ex.RequestTrailers)
	d.ResponseTrailers = toHeaderDTOs(ex.ResponseTrailers)
	d.WebSocketMessages = make([]wsMessageDTO, 0, len(ex.WebSocketMessages))
	for _, m := range ex.WebSocketMessages {
		data := m.DataString
		if data == "" && len(m.Data) > 0 {
			data = string(m.Data)
		}
		d.WebSocketMessages = append(d.WebSocketMessages, wsMessageDTO{
			ID: m.ID, Direction: m.Direction, OpCode: m.OpCode, Length: m.Length,
			WrittenLength: m.WrittenLength, Data: data, FrameCount: m.FrameCount,
			MessageStart: formatTime(m.MessageStart), MessageEnd: formatTime(m.MessageEnd),
		})
	}

	m := ex.Metrics
	d.Metrics = metricsDTO{
		StartedAt: formatTime(m.StartedAt()), DurationMs: m.Duration().Milliseconds(),
		TotalSent: m.TotalSent, TotalReceived: m.TotalReceived,
		RequestHeaderLength: m.RequestHeaderLength, ResponseHeaderLength: m.ResponseHeaderLength,
		ReusingConnection:    m.ReusingConnection,
		DownStreamClientAddr: m.DownStreamClientAddress, DownStreamClientPort: m.DownStreamClientPort,
		DownStreamLocalAddr: m.DownStreamLocalAddress, DownStreamLocalPort: m.DownStreamLocalPort,
		Times: map[string]string{},
	}
	for k, v := range map[string]time.Time{
		"receivedFromProxy":    m.ReceivedFromProxy,
		"retrievingPool":       m.RetrievingPool,
		"requestHeaderSending": m.RequestHeaderSending,
		"requestHeaderSent":    m.RequestHeaderSent,
		"requestBodySent":      m.RequestBodySent,
		"responseHeaderStart":  m.ResponseHeaderStart,
		"responseHeaderEnd":    m.ResponseHeaderEnd,
		"responseBodyStart":    m.ResponseBodyStart,
		"responseBodyEnd":      m.ResponseBodyEnd,
	} {
		if !v.IsZero() {
			d.Metrics.Times[k] = v.Format(time.RFC3339Nano)
		}
	}
}

func toHeaderDTOs(in []archive.HeaderField) []headerDTO {
	out := make([]headerDTO, 0, len(in))
	for _, h := range in {
		out = append(out, headerDTO{Name: h.Name, Value: h.Value, Forwarded: h.Forwarded})
	}
	return out
}

func toConnectionDTO(c *archive.Connection) *connectionDTO {
	return &connectionDTO{
		ID: c.ID, HTTPVersion: c.HTTPVersion,
		HostName: c.Authority.HostName, Port: c.Authority.Port, Secure: c.Authority.Secure,
		RequestProcessed: c.RequestProcessed, LocalPort: c.LocalPort,
		LocalAddress: c.LocalAddress, RemoteAddress: c.RemoteAddress,
		DNSStart: formatTime(c.DnsSolveStart), DNSEnd: formatTime(c.DnsSolveEnd),
		TcpOpening: formatTime(c.TcpConnectionOpening), TcpOpened: formatTime(c.TcpConnectionOpened),
		SslStart: formatTime(c.SslNegotiationStart), SslEnd: formatTime(c.SslNegotiationEnd),
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func bodyInfo(dir string, id int64) bodyInfoDTO {
	out := bodyInfoDTO{}
	if info, err := os.Stat(filepath.Join(dir, "contents", fmt.Sprintf("req-%d.data", id))); err == nil {
		out.RequestExists = true
		out.RequestSize = info.Size()
	}
	if info, err := os.Stat(filepath.Join(dir, "contents", fmt.Sprintf("res-%d.data", id))); err == nil {
		out.ResponseExists = true
		out.ResponseSize = info.Size()
	}
	return out
}

func (h *Handlers) detectBodyHeaders(dir string, id int64, kind string) (contentType, contentEncoding string) {
	ex, err := readExchangeMPack(dir, id)
	if err != nil {
		return "", ""
	}
	var headers []archive.HeaderField
	if kind == "response" {
		if ex.Response != nil {
			headers = ex.Response.Headers
		}
	} else {
		headers = ex.Request.Headers
	}
	for _, hd := range headers {
		switch {
		case strings.EqualFold(hd.Name, "content-type"):
			contentType = hd.Value
		case strings.EqualFold(hd.Name, "content-encoding"):
			contentEncoding = hd.Value
		}
	}
	return contentType, contentEncoding
}

func sniffContentType(f *os.File) string {
	buf := make([]byte, 512)
	n, _ := f.ReadAt(buf, 0)
	if n <= 0 {
		return ""
	}
	ct := http.DetectContentType(buf[:n])
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

func isTextualContentType(ct string) bool {
	ct = strings.ToLower(ct)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/javascript",
		"application/x-javascript", "application/x-www-form-urlencoded",
		"application/graphql", "application/ld+json":
		return true
	}
	return strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml")
}
