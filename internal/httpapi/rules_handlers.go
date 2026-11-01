package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vacks/fluxzy-console/internal/rules"
)

func (h *Handlers) rulesStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.deps.Rules.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取规则状态失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) rulesSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.deps.Rules.Schema())
}

func (h *Handlers) rulesVersions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	versions, err := h.deps.Store.ListConfigVersions(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取版本失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": versions})
}

func (h *Handlers) rulesVersion(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	v, err := h.deps.Store.GetConfigVersion(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "版本不存在", nil)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type yamlRequest struct {
	YAML string `json:"yaml"`
}

func (h *Handlers) rulesValidate(w http.ResponseWriter, r *http.Request) {
	var req yamlRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	result := h.deps.Rules.Validate(r.Context(), req.YAML)
	writeJSON(w, http.StatusOK, result)
}

type renderRule struct {
	Name    string           `json:"name"`
	Enabled *bool            `json:"enabled"`
	Filter  map[string]any   `json:"filter"`
	Actions []map[string]any `json:"actions"`
}

type renderRequest struct {
	Rules []renderRule `json:"rules"`
}

func (h *Handlers) rulesRender(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	model := rules.RuleModel{Rules: make([]rules.RuleDraft, 0, len(req.Rules))}
	for _, rr := range req.Rules {
		enabled := true
		if rr.Enabled != nil {
			enabled = *rr.Enabled
		}
		model.Rules = append(model.Rules, rules.RuleDraft{
			Name: rr.Name, Enabled: enabled, Filter: rr.Filter, Actions: rr.Actions,
		})
	}
	yamlText, err := h.deps.Rules.Render(model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "render_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": yamlText})
}

func (h *Handlers) rulesParse(w http.ResponseWriter, r *http.Request) {
	var req yamlRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	model, err := h.deps.Rules.Parse([]byte(req.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

type saveRuleRequest struct {
	YAML string `json:"yaml"`
	Note string `json:"note"`
}

func (h *Handlers) rulesSave(w http.ResponseWriter, r *http.Request) {
	var req saveRuleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	version, result, err := h.deps.Rules.Save(r.Context(), req.YAML, req.Note, currentUser(r))
	if err != nil {
		if errors.Is(err, rules.ErrInvalidRules) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":      apiError{Code: "invalid_rules", Message: "规则校验未通过", Details: result},
				"validation": result,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "保存规则失败: "+err.Error(), nil)
		return
	}
	h.audit(r, "rules.save", strconv.FormatInt(version.ID, 10), map[string]any{
		"hash": version.YAMLHash, "note": req.Note,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    version,
		"validation": result,
		"pending":    true,
		"message":    "规则已保存，尚未应用。点击“保存并应用”热更新到 fluxzy。",
	})
}

func (h *Handlers) rulesApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExpectedHash string `json:"expectedHash"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := h.deps.Rules.Apply(ctx, req.ExpectedHash, currentUser(r))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, rules.ErrInvalidRules) {
			status = http.StatusUnprocessableEntity
		}
		if result != nil {
			h.audit(r, "rules.apply_failed", "", map[string]any{
				"error": err.Error(), "rolledBack": result.RolledBack,
			})
			writeJSON(w, status, map[string]any{
				"error":   apiError{Code: "apply_failed", Message: err.Error()},
				"result":  result,
				"message": result.Message,
			})
			return
		}
		h.audit(r, "rules.apply_failed", "", map[string]any{"error": err.Error()})
		writeError(w, status, "apply_failed", err.Error(), nil)
		return
	}
	versionID := int64(0)
	if result.Version != nil {
		versionID = result.Version.ID
	}
	h.audit(r, "rules.apply", strconv.FormatInt(versionID, 10), map[string]any{
		"hash": result.Version.YAMLHash, "sessionId": result.ActiveSessionID,
	})
	writeJSON(w, http.StatusOK, result)
}

type rollbackRequest struct {
	VersionID int64 `json:"versionId"`
}

func (h *Handlers) rulesRollback(w http.ResponseWriter, r *http.Request) {
	var req rollbackRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	version, result, err := h.deps.Rules.Rollback(r.Context(), req.VersionID, currentUser(r))
	if err != nil {
		if errors.Is(err, rules.ErrInvalidRules) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":      apiError{Code: "invalid_rules", Message: "历史版本校验未通过", Details: result},
				"validation": result,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "回滚失败: "+err.Error(), nil)
		return
	}
	h.audit(r, "rules.rollback", strconv.FormatInt(version.ID, 10), map[string]any{
		"sourceVersionId": req.VersionID,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    version,
		"validation": result,
		"pending":    true,
		"message":    "回滚版本已保存，尚未应用。点击“保存并应用”热更新后生效。",
	})
}
