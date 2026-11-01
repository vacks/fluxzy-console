package httpapi

import (
	"net/http"
	"time"

	"github.com/vacks/fluxzy-console/internal/retention"
)

type retentionSettingsRequest struct {
	HotHours      *int `json:"hotHours"`
	ColdDays      *int `json:"coldDays"`
	SamplePercent *int `json:"samplePercent"`
}

func (h *Handlers) retentionDefaults() retention.Settings {
	return retention.Settings{
		HotHours:      int(h.deps.Cfg.HotRetention / time.Hour),
		ColdDays:      int(h.deps.Cfg.ColdRetention / (24 * time.Hour)),
		SamplePercent: h.deps.Cfg.SamplePercent,
	}
}

func (h *Handlers) getRetentionSettings(w http.ResponseWriter, r *http.Request) {
	settings := h.retentionDefaults()
	if h.deps.Retention != nil {
		settings = h.deps.Retention.Settings()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings,
		"defaults": h.retentionDefaults(),
		"limits": map[string]any{
			"hotHours":      map[string]int{"min": 0, "max": 8760},
			"coldDays":      map[string]int{"min": 0, "max": 3650},
			"samplePercent": map[string]int{"min": 0, "max": 100},
		},
		"applyMode": "next_hourly_run",
	})
}

func (h *Handlers) updateRetentionSettings(w http.ResponseWriter, r *http.Request) {
	if h.deps.Retention == nil {
		writeError(w, http.StatusServiceUnavailable, "retention_unavailable", "保留策略服务未启动", nil)
		return
	}
	var req retentionSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.HotHours == nil || req.ColdDays == nil || req.SamplePercent == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "hotHours、coldDays、samplePercent 均为必填", nil)
		return
	}
	next := retention.Settings{
		HotHours:      *req.HotHours,
		ColdDays:      *req.ColdDays,
		SamplePercent: *req.SamplePercent,
	}
	old := h.deps.Retention.Settings()
	if err := h.deps.Retention.UpdateSettings(r.Context(), next); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error(), nil)
		return
	}
	h.audit(r, "settings.retention", "", map[string]any{"old": old, "new": next})
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":  next,
		"applyMode": "next_hourly_run",
		"message":   "保留策略已保存，将在下一次清理周期生效；已删除的数据无法恢复。",
	})
}
