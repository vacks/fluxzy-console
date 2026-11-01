package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vacks/fluxzy-console/internal/auth"
	"github.com/vacks/fluxzy-console/internal/store"
)

type userDTO struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   int64  `json:"createdAt"`
	LastLoginAt *int64 `json:"lastLoginAt,omitempty"`
}

func toUserDTO(u *store.User) *userDTO {
	if u == nil {
		return nil
	}
	return &userDTO{
		ID:          u.ID,
		Username:    u.Username,
		Role:        u.Role,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

type authStatusResponse struct {
	SetupRequired bool     `json:"setupRequired"`
	Authenticated bool     `json:"authenticated"`
	User          *userDTO `json:"user,omitempty"`
	CSRF          string   `json:"csrfToken,omitempty"`
}

func (h *Handlers) authStatus(w http.ResponseWriter, r *http.Request) {
	resp := authStatusResponse{}
	required, err := h.deps.Auth.SetupRequired(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取用户状态失败", nil)
		return
	}
	resp.SetupRequired = required

	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		u, as, err := h.deps.Auth.Authenticate(r.Context(), c.Value)
		if err == nil {
			resp.Authenticated = true
			resp.User = toUserDTO(u)
			resp.CSRF = as.CSRFToken
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User *userDTO `json:"user"`
	CSRF string   `json:"csrfToken"`
}

func sameOriginRequest(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		writeError(w, http.StatusForbidden, "cross_origin_forbidden", "不允许跨站登录", nil)
		return
	}
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	ip := clientIP(r, h.deps.Cfg.TrustProxyHeaders)
	loginCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := h.deps.Auth.Login(loginCtx, req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		if !errors.Is(err, auth.ErrTooManyAttempts) {
			h.auditLoginFailure(r, req.Username, err)
		}
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error(), nil)
		case errors.Is(err, auth.ErrDisabled), errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error(), nil)
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "登录失败", nil)
		}
		return
	}
	h.setSessionCookie(w, r, result.Token, result.ExpiresAt)
	h.auditAs(r, result.User, "auth.login", result.User.Username, nil)
	writeJSON(w, http.StatusOK, loginResponse{User: toUserDTO(result.User), CSRF: result.CSRF})
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) setup(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		writeError(w, http.StatusForbidden, "cross_origin_forbidden", "不允许跨站初始化", nil)
		return
	}
	var req setupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	u, err := h.deps.Auth.Setup(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSetupComplete):
			writeError(w, http.StatusConflict, "setup_complete", err.Error(), nil)
		case errors.Is(err, auth.ErrValidation):
			writeError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "初始化失败", nil)
		}
		return
	}
	result, err := h.deps.Auth.Login(r.Context(), req.Username, req.Password,
		clientIP(r, h.deps.Cfg.TrustProxyHeaders), r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "账号已创建，但自动登录失败", nil)
		return
	}
	h.setSessionCookie(w, r, result.Token, result.ExpiresAt)
	h.auditAs(r, u, "auth.setup", u.Username, nil)
	writeJSON(w, http.StatusOK, loginResponse{User: toUserDTO(u), CSRF: result.CSRF})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(ctxToken).(string)
	u := currentUser(r)
	h.deps.Auth.Logout(r.Context(), token)
	h.clearSessionCookie(w, r)
	if u != nil {
		h.audit(r, "auth.logout", u.Username, nil)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	as := currentAuth(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      toUserDTO(u),
		"csrfToken": as.CSRFToken,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (h *Handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	u := currentUser(r)
	if err := h.deps.Auth.ChangePassword(r.Context(), u.ID, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "invalid_password", "当前密码不正确", nil)
		case errors.Is(err, auth.ErrValidation):
			writeError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "修改密码失败", nil)
		}
		return
	}
	h.clearSessionCookie(w, r)
	h.audit(r, "auth.password_change", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relogin": true})
}

func (h *Handlers) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	secure := h.deps.Cfg.CookieSecure
	if !secure && h.deps.Cfg.TrustProxyHeaders {
		secure = strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handlers) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.deps.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handlers) auditLoginFailure(r *http.Request, username string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = h.deps.Store.AddAudit(ctx, store.AuditEntry{
		Username:  username,
		Action:    "auth.login_failed",
		Target:    username,
		Detail:    err.Error(),
		IP:        clientIP(r, h.deps.Cfg.TrustProxyHeaders),
		UserAgent: r.UserAgent(),
	})
}

// ---- user management ----

func (h *Handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.deps.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "读取用户列表失败", nil)
		return
	}
	out := make([]*userDTO, 0, len(users))
	for i := range users {
		out = append(out, toUserDTO(&users[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := auth.ValidateUsername(strings.TrimSpace(req.Username)); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建用户失败", nil)
		return
	}
	u, err := h.deps.Store.CreateUser(r.Context(), strings.TrimSpace(req.Username), hash)
	if errors.Is(err, store.ErrUsernameExists) {
		writeError(w, http.StatusConflict, "conflict", "用户名已存在", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "创建用户失败", nil)
		return
	}
	h.audit(r, "user.create", u.Username, map[string]any{"role": "admin"})
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserDTO(u)})
}

type updateUserRequest struct {
	Disabled *bool `json:"disabled"`
}

func (h *Handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	h.userMu.Lock()
	defer h.userMu.Unlock()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	target, err := h.deps.Store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "用户不存在", nil)
		return
	}
	disabled := target.Disabled
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	if target.Role == "admin" && disabled {
		admins, _ := h.countActiveAdmins(r.Context())
		if admins <= 1 {
			writeError(w, http.StatusConflict, "last_admin", "不能禁用最后一个管理员", nil)
			return
		}
	}
	if err := h.deps.Store.UpdateUser(r.Context(), id, disabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "更新用户失败", nil)
		return
	}
	if disabled {
		_ = h.deps.Store.DeleteUserAuthSessions(r.Context(), id)
	}
	h.audit(r, "user.update", target.Username, map[string]any{"role": "admin", "disabled": disabled})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	h.userMu.Lock()
	defer h.userMu.Unlock()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	u := currentUser(r)
	if u.ID == id {
		writeError(w, http.StatusConflict, "self_delete", "不能删除当前登录账号", nil)
		return
	}
	target, err := h.deps.Store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "用户不存在", nil)
		return
	}
	if target.Role == "admin" {
		admins, _ := h.countActiveAdmins(r.Context())
		if admins <= 1 {
			writeError(w, http.StatusConflict, "last_admin", "不能删除最后一个管理员", nil)
			return
		}
	}
	_ = h.deps.Store.DeleteUserAuthSessions(r.Context(), id)
	if err := h.deps.Store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "删除用户失败", nil)
		return
	}
	h.audit(r, "user.delete", target.Username, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) countActiveAdmins(ctx context.Context) (int, error) {
	users, err := h.deps.Store.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u.Role == "admin" && !u.Disabled {
			n++
		}
	}
	return n, nil
}
