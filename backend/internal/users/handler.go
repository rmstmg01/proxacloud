package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/mailer"
	"proxacloud/backend/internal/rbac"
)

type Handler struct {
	users        *Service
	tokens       *auth.TokenService
	mailer       mailer.Sender
	publicAppURL string
}

func NewHandler(users *Service, tokens *auth.TokenService, sender mailer.Sender, publicAppURL string) *Handler {
	return &Handler{users: users, tokens: tokens, mailer: sender, publicAppURL: publicAppURL}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	user, err := h.users.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	if started, err := h.beginLoginTwoFactor(w, r, user); started {
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start two-factor verification"})
		}
		return
	}

	h.users.setLastLogin(user.ID, clientIP(r))

	token, err := h.tokens.Issue(user.Username, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user":  user,
	})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		FullName      string `json:"full_name"`
		Email         string `json:"email"`
		ContactNumber string `json:"contact_number"`
		Password      string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	result, err := h.users.RegisterClient(req.Username, req.Password, ProfileInput{
		FullName:      req.FullName,
		Email:         req.Email,
		ContactNumber: req.ContactNumber,
	}, h.publicAppURL)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	body := "Welcome to ProxaCloud.\n\nVerify your email address using this link:\n\n" + result.VerificationURL + "\n\nAfter verification, an admin must approve your account before you can create resources."
	if h.mailer != nil {
		_ = h.mailer.Send(result.User.Email, "Verify your ProxaCloud account", body)
	}
	payload := map[string]any{
		"user":    result.User,
		"message": "Registration submitted. Please verify your email, then wait for admin approval.",
	}
	if h.mailer == nil || !h.mailer.Enabled() {
		payload["verification_url"] = result.VerificationURL
	}
	writeJSON(w, http.StatusCreated, payload)
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		var req struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		token = strings.TrimSpace(req.Token)
	}
	user, err := h.users.VerifyEmail(token)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":    user,
		"message": "Email verified. Your account is waiting for admin approval.",
	})
}

func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	result, found, err := h.users.RequestPasswordReset(req.Email, h.publicAppURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if found && h.mailer != nil {
		body := "We received a request to reset your ProxaCloud password.\n\nUse this link to set a new password:\n\n" + result.ResetURL + "\n\nThis link expires in 1 hour. If you did not request this, you can ignore this email."
		_ = h.mailer.Send(result.Email, "Reset your ProxaCloud password", body)
	}
	payload := map[string]any{"message": "If an account exists for that email, a password reset link has been sent."}
	if found && (h.mailer == nil || !h.mailer.Enabled()) {
		payload["reset_url"] = result.ResetURL
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.users.ResetPassword(req.Token, req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Password has been reset. You can sign in now."})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	user, exists := h.users.GetByUsername(ctxUser.Username)
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Username        string `json:"username"`
		FirstName       string `json:"first_name"`
		LastName        string `json:"last_name"`
		ContactNumber   string `json:"contact_number"`
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	user, err := h.users.UpdateOwnProfile(ctxUser.Username, SelfProfileInput{
		Username:        req.Username,
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		ContactNumber:   req.ContactNumber,
		CurrentPassword: req.CurrentPassword,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is required when changing username"})
		case errors.Is(err, ErrUserExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrUserNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}
	token, err := h.tokens.Issue(user.Username, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.users.ChangeOwnPassword(ctxUser.Username, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		FullName      string `json:"full_name"`
		Email         string `json:"email"`
		ContactNumber string `json:"contact_number"`
		Password      string `json:"password"`
		Role          string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	role := rbac.Role(req.Role)
	user, err := h.users.CreateUser(req.Username, req.Password, role, ProfileInput{
		FullName:      req.FullName,
		Email:         req.Email,
		ContactNumber: req.ContactNumber,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUserExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrInvalidRole):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}

	var req struct {
		FullName      string `json:"full_name"`
		Email         string `json:"email"`
		ContactNumber string `json:"contact_number"`
		Role          string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	user, err := h.users.UpdateUser(id, rbac.Role(req.Role), ProfileInput{
		FullName:      req.FullName,
		Email:         req.Email,
		ContactNumber: req.ContactNumber,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUserExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrUserNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrInvalidRole):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	var req struct {
		ConfirmationUsername string `json:"confirmation_username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	ctxUser, _ := auth.UserFromContext(r.Context())
	if err := h.users.DeleteUser(id, req.ConfirmationUsername, ctxUser.Username); err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrDeleteSelf), errors.Is(err, ErrLastSuperAdmin):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	ctxUser, _ := auth.UserFromContext(r.Context())
	user, err := h.users.SetStatus(id, Status(req.Status), ctxUser.Username, req.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) ListUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.users.ListUsers())
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
