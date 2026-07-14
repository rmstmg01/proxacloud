package vps

import (
	"encoding/json"
	"errors"
	"io"
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
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	if scope.IsCustomer() {
		req.CustomerAccountID = scope.CustomerAccountID
		if h.billing != nil {
			if err := h.billing.EnsureCanAfford(scope.CustomerAccountID, "vps", req.CPU, req.MemoryMB, req.DiskGB); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, billing.ErrInsufficientCredit) {
					status = http.StatusPaymentRequired
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	resp, err := h.service.Create(req)
	if err != nil {
		if errors.Is(err, ErrVPSExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	scope, ok := h.authorizeInstance(w, r, id)
	if !ok {
		return
	}
	resp, err := h.service.Update(id, req, scope)
	if err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrVPSExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateHostname(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req UpdateHostnameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	scope, ok := h.authorizeInstance(w, r, id)
	if !ok {
		return
	}
	resp, err := h.service.UpdateHostname(id, req, scope)
	if err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.Deploy(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Redeploy(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.Redeploy(id)
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

func (h *Handler) Shutdown(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "shutdown")
}

func (h *Handler) RefreshStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.RefreshStatus(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Console(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.Console(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) LoginDetails(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.LoginDetails(id)
	if err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ResetRootPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req ResetRootPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.ResetRootPassword(id, req)
	if err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ConsoleWebSocket(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.service.ProxyConsole(w, r, id); err != nil {
		return
	}
}

func (h *Handler) powerAction(w http.ResponseWriter, r *http.Request, action string) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.PowerAction(id, action)
	if err != nil {
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
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	if err := h.service.Delete(id, req.ConfirmationName); err != nil {
		if errors.Is(err, ErrVPSNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.ListEvents(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetTunnel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.GetTunnelAttachment(id)
	if err != nil {
		if errors.Is(err, ErrTunnelAttachmentNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RetryTunnel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, ok := h.authorizeInstance(w, r, id); !ok {
		return
	}
	resp, err := h.service.RetryTunnel(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListImages(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.ListImages()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateImage(w http.ResponseWriter, r *http.Request) {
	var req CreateCloudInitImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.CreateImage(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) DiscoverImages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CredentialID int64  `json:"proxmox_credential_id"`
		NodeName     string `json:"node_name"`
		RegionID     int64  `json:"region_id"`
		StorageID    string `json:"storage_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	var resp []CloudInitImage
	var err error
	if req.RegionID > 0 {
		resp, err = h.service.DiscoverImagesForRegion(req.RegionID, req.StorageID)
	} else if req.CredentialID <= 0 && strings.TrimSpace(req.NodeName) == "" {
		resp, err = h.service.DiscoverImagesAutomatically(req.StorageID)
	} else {
		resp, err = h.service.DiscoverImages(req.CredentialID, req.NodeName, req.StorageID)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) authorizeInstance(w http.ResponseWriter, r *http.Request, id int64) (tenant.Scope, bool) {
	scope, ok := h.scope(w, r)
	if !ok {
		return tenant.Scope{}, false
	}
	if _, err := h.service.Get(id, scope); err != nil {
		if errors.Is(err, ErrVPSNotFound) {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid vps id"})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
