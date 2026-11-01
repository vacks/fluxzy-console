package httpapi

import (
	"net/http"

	"github.com/vacks/fluxzy-console/internal/trustca"
)

func (h *Handlers) caStatus(w http.ResponseWriter, r *http.Request) {
	if h.deps.CA == nil {
		writeError(w, http.StatusServiceUnavailable, "ca_unavailable", "CA 管理未启动", nil)
		return
	}
	info, err := h.deps.CA.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ca_status_failed", "读取 CA 状态失败: "+err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"info": info})
}

func (h *Handlers) caUpload(w http.ResponseWriter, r *http.Request) {
	if h.deps.CA == nil {
		writeError(w, http.StatusServiceUnavailable, "ca_unavailable", "CA 管理未启动", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, trustca.MaxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(trustca.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "无法读取上传文件: "+err.Error(), nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "缺少 file 字段", nil)
		return
	}
	defer file.Close()
	info, err := h.deps.CA.Upload(r.Context(), file)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "ca_upload_failed", err.Error(), nil)
		return
	}
	h.audit(r, "system.ca_upload", header.Filename, map[string]any{
		"source": info.Source, "subject": info.Subject, "fingerprint": info.FingerprintSHA256,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "info": info,
		"message": "自定义 CA 已生效；fluxzy 已重启，客户端需要信任新的 CA。",
	})
}

func (h *Handlers) caRegenerate(w http.ResponseWriter, r *http.Request) {
	if h.deps.CA == nil {
		writeError(w, http.StatusServiceUnavailable, "ca_unavailable", "CA 管理未启动", nil)
		return
	}
	var req struct {
		CommonName string `json:"commonName"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	info, err := h.deps.CA.Regenerate(r.Context(), req.CommonName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ca_regenerate_failed", err.Error(), nil)
		return
	}
	h.audit(r, "system.ca_regenerate", "", map[string]any{
		"subject": info.Subject, "fingerprint": info.FingerprintSHA256,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "info": info,
		"message": "新 CA 已生成并生效；fluxzy 已重启，客户端需要重新信任新的 CA。",
	})
}

func (h *Handlers) caReset(w http.ResponseWriter, r *http.Request) {
	if h.deps.CA == nil {
		writeError(w, http.StatusServiceUnavailable, "ca_unavailable", "CA 管理未启动", nil)
		return
	}
	info, err := h.deps.CA.Reset(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ca_reset_failed", err.Error(), nil)
		return
	}
	h.audit(r, "system.ca_reset", "", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "info": info,
		"message": "已恢复 fluxzy 内置 CA；fluxzy 已重启。",
	})
}
