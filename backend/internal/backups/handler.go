package backups

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/billing"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

type Handler struct {
	service  *Service
	resolver interface {
		TenantScope(username string, role rbac.Role) (tenant.Scope, error)
	}
}

func NewHandler(service *Service, resolver interface {
	TenantScope(username string, role rbac.Role) (tenant.Scope, error)
}) *Handler {
	return &Handler{service: service, resolver: resolver}
}

func (h *Handler) Summary(w http.ResponseWriter, _ *http.Request) {
	summary, err := h.service.Summary()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) ListTargets(w http.ResponseWriter, _ *http.Request) {
	targets, err := h.service.ListTargets()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (h *Handler) ListJobs(w http.ResponseWriter, _ *http.Request) {
	jobs, err := h.service.ListJobs()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (h *Handler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	var req TargetInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	req.EnabledSet = true
	target, err := h.service.CreateTarget(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, target)
}

func (h *Handler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid backup target id"})
		return
	}
	if err := h.service.DeleteTarget(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) StartJob(w http.ResponseWriter, r *http.Request) {
	var req JobInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	job, err := h.service.StartJob(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (h *Handler) VPSOverview(w http.ResponseWriter, r *http.Request) {
	id, scope, ok := h.vpsScope(w, r)
	if !ok {
		return
	}
	overview, err := h.service.GetVPSBackupOverview(id, scope)
	if err != nil {
		h.writeVPSBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (h *Handler) UpsertVPSPolicy(w http.ResponseWriter, r *http.Request) {
	id, scope, ok := h.vpsScope(w, r)
	if !ok {
		return
	}
	var req UpsertVPSBackupPolicyInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	policy, err := h.service.UpsertVPSBackupPolicy(id, scope, req)
	if err != nil {
		h.writeVPSBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) DisableVPSPolicy(w http.ResponseWriter, r *http.Request) {
	id, scope, ok := h.vpsScope(w, r)
	if !ok {
		return
	}
	if err := h.service.DisableVPSBackupPolicy(id, scope); err != nil {
		h.writeVPSBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vps_id": id, "enabled": false})
}

func (h *Handler) StartVPSBackup(w http.ResponseWriter, r *http.Request) {
	id, scope, ok := h.vpsScope(w, r)
	if !ok {
		return
	}
	job, err := h.service.StartManualVPSBackup(id, scope)
	if err != nil {
		h.writeVPSBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) vpsScope(w http.ResponseWriter, r *http.Request) (int64, tenant.Scope, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid VPS id"})
		return 0, tenant.Scope{}, false
	}
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return 0, tenant.Scope{}, false
	}
	if h.resolver == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "tenant resolver is not configured"})
		return 0, tenant.Scope{}, false
	}
	scope, err := h.resolver.TenantScope(ctxUser.Username, ctxUser.Role)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return 0, tenant.Scope{}, false
	}
	return id, scope, true
}

func (h *Handler) writeVPSBackupError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, billing.ErrInsufficientCredit):
		status = http.StatusPaymentRequired
	case errors.Is(err, ErrBackupInProgress):
		status = http.StatusConflict
	case strings.Contains(strings.ToLower(err.Error()), "not found"):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
