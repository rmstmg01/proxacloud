package databaseclusters

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/billing"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

type Handler struct {
	service *Service
	billing interface {
		EnsureCanAfford(accountID int64, resourceType string, cpu, memoryMB, diskGB int) error
	}
	resolver interface {
		TenantScope(username string, role rbac.Role) (tenant.Scope, error)
	}
}

type DeleteRequest struct {
	ConfirmationName string `json:"confirmation_name"`
}

func NewHandler(service *Service, resolver interface {
	TenantScope(username string, role rbac.Role) (tenant.Scope, error)
}, billingGuard interface {
	EnsureCanAfford(accountID int64, resourceType string, cpu, memoryMB, diskGB int) error
}) *Handler {
	return &Handler{service: service, resolver: resolver, billing: billingGuard}
}

func (h *Handler) Plan(w http.ResponseWriter, r *http.Request) {
	var req PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.Plan(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Provision(w http.ResponseWriter, r *http.Request) {
	var req ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	if scope.IsCustomer() {
		if req.RegionID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "region_id is required"})
			return
		}
		req.CustomerAccountID = scope.CustomerAccountID
		if h.billing != nil {
			cpu, memoryMB, diskGB := req.CPU, req.MemoryMB, req.DiskGB
			if cpu <= 0 {
				cpu = 4
			}
			if memoryMB <= 0 {
				memoryMB = 8192
			}
			if diskGB <= 0 {
				diskGB = 100
			}
			if err := h.billing.EnsureCanAfford(scope.CustomerAccountID, "database_cluster", cpu*3, memoryMB*3, diskGB*3); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, billing.ErrInsufficientCredit) {
					status = http.StatusPaymentRequired
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	resp, err := h.service.ProvisionAndBootstrap(req)
	if err != nil {
		if errors.Is(err, ErrDatabaseClusterExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) Credentials(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeCluster(w, r, id); !ok {
		return
	}
	resp, err := h.service.Credentials(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.Bootstrap(id, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "start")
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "restart")
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "stop")
}

func (h *Handler) ApplyTerraform(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.ApplyTerraform(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	resp, err := h.service.List(scope)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	resp, err := h.service.Get(id, scope)
	if err != nil {
		if errors.Is(err, ErrDatabaseClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(id, req.ConfirmationName, scope); err != nil {
		if errors.Is(err, ErrDatabaseClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) powerAction(w http.ResponseWriter, r *http.Request, action string) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	scope, ok := h.authorizeCluster(w, r, id)
	if !ok {
		return
	}
	resp, err := h.service.PowerAction(id, action, scope)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeCluster(w, r, id); !ok {
		return
	}
	resp, err := h.service.ListRuns(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeCluster(w, r, id); !ok {
		return
	}
	resp, err := h.service.ListEvents(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) KubernetesManifest(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeCluster(w, r, id); !ok {
		return
	}
	resp, err := h.service.KubernetesManifest(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) authorizeCluster(w http.ResponseWriter, r *http.Request, id int64) (tenant.Scope, bool) {
	scope, ok := h.scope(w, r)
	if !ok {
		return tenant.Scope{}, false
	}
	if _, err := h.service.Get(id, scope); err != nil {
		if errors.Is(err, ErrDatabaseClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return tenant.Scope{}, false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return tenant.Scope{}, false
	}
	return scope, true
}

func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (tenant.Scope, bool) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return tenant.Scope{}, false
	}
	scope, err := h.resolver.TenantScope(ctxUser.Username, ctxUser.Role)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return tenant.Scope{}, false
	}
	return scope, true
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid database cluster id"})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
