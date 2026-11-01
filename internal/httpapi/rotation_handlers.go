package httpapi

import (
	"net/http"

	"github.com/vacks/fluxzy-console/internal/rotation"
)

func (h *Handlers) rotationSettings(w http.ResponseWriter, r *http.Request) {
	if h.deps.Rotation == nil {
		writeError(w, http.StatusServiceUnavailable, "rotation_unavailable", "轮转服务未启动", nil)
		return
	}
	st := h.deps.Rotation.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":        st.Settings,
		"defaults":        st.Defaults,
		"disk":            st.Disk,
		"activeSessionId": st.ActiveSessionID,
		"sessionBytes":    st.SessionBytes,
		"lastRotatedAt":   st.LastRotatedAt,
		"lastReason":      st.LastReason,
		"nextCheckAt":     st.NextCheckAt,
		"applyMode":       "next_check",
		"limits": map[string]any{
			"intervalHours":      map[string]int{"min": 0, "max": 8760},
			"checkSeconds":       map[string]int{"min": 5, "max": 3600},
			"maxExchanges":       map[string]int{"min": 0},
			"maxSessionBytes":    map[string]int{"min": 0},
			"minFreeBytes":       map[string]int{"min": 0},
			"emergencyFreeBytes": map[string]int{"min": 0},
		},
	})
}

func (h *Handlers) rotationUpdate(w http.ResponseWriter, r *http.Request) {
	if h.deps.Rotation == nil {
		writeError(w, http.StatusServiceUnavailable, "rotation_unavailable", "轮转服务未启动", nil)
		return
	}
	var next rotation.Settings
	if err := decodeJSON(w, r, &next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := h.deps.Rotation.UpdateSettings(r.Context(), next); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error(), nil)
		return
	}
	h.audit(r, "fluxzy.rotation.update", "", next)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":  next,
		"applyMode": "next_check",
		"message":   "轮转策略已保存；下一次检查周期生效。轮转会重启 fluxzy 并创建新会话。",
	})
}
