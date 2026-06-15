package networks

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListVPCs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.service.ListVPCs())
}

func (h *Handler) CreateVPC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		CIDR        string `json:"cidr"`
		Description string `json:"description"`
		IsDefault   bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	created, err := h.service.CreateVPC(VPC{
		Name:        req.Name,
		CIDR:        req.CIDR,
		Description: req.Description,
		IsDefault:   req.IsDefault,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.service.List())
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VPCID     int64    `json:"vpc_id"`
		Name      string   `json:"name"`
		CIDR      string   `json:"cidr"`
		Gateway   string   `json:"gateway"`
		DNS       []string `json:"dns"`
		Bridge    string   `json:"bridge"`
		VLAN      int      `json:"vlan"`
		IsDefault bool     `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	created, err := h.service.Create(Profile{
		VPCID:     req.VPCID,
		Name:      req.Name,
		CIDR:      req.CIDR,
		Gateway:   req.Gateway,
		DNS:       req.DNS,
		Bridge:    req.Bridge,
		VLAN:      req.VLAN,
		IsDefault: req.IsDefault,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network profile id"})
		return
	}

	var req struct {
		VPCID   int64    `json:"vpc_id"`
		Name    string   `json:"name"`
		CIDR    string   `json:"cidr"`
		Gateway string   `json:"gateway"`
		DNS     []string `json:"dns"`
		Bridge  string   `json:"bridge"`
		VLAN    int      `json:"vlan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	updated, err := h.service.Update(id, Profile{
		VPCID:   req.VPCID,
		Name:    req.Name,
		CIDR:    req.CIDR,
		Gateway: req.Gateway,
		DNS:     req.DNS,
		Bridge:  req.Bridge,
		VLAN:    req.VLAN,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrNetworkNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrVPCNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) SetDefault(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network profile id"})
		return
	}

	updated, err := h.service.SetDefault(id)
	if err != nil {
		if errors.Is(err, ErrNetworkNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) ListNodeTags(w http.ResponseWriter, _ *http.Request) {
	tags, err := h.service.ListNodeTags()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (h *Handler) UpsertNodeTag(w http.ResponseWriter, r *http.Request) {
	nodeName := chi.URLParam(r, "nodeName")
	if nodeName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid proxmox node name"})
		return
	}

	var req struct {
		NetworkProfileID int64 `json:"network_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	tag, err := h.service.UpsertNodeTag(nodeName, req.NetworkProfileID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNetworkNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, tag)
}

func (h *Handler) DeleteNodeTag(w http.ResponseWriter, r *http.Request) {
	nodeName := chi.URLParam(r, "nodeName")
	if nodeName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid proxmox node name"})
		return
	}

	if err := h.service.DeleteNodeTag(nodeName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"proxmox_node_name": nodeName, "deleted": true})
}

func (h *Handler) ListReservedIPs(w http.ResponseWriter, r *http.Request) {
	networkID, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network profile id"})
		return
	}

	reserved, err := h.service.GetReservedIPsByNetworkProfileID(networkID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, reserved)
}

func (h *Handler) CreateReservedIP(w http.ResponseWriter, r *http.Request) {
	networkID, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network profile id"})
		return
	}

	var req struct {
		Hostname    string `json:"hostname"`
		IPAddress   string `json:"ip_address"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	reserved, err := h.service.CreateReservedIP(networkID, req.Hostname, req.IPAddress, req.Description)
	if err != nil {
		switch {
		case errors.Is(err, ErrNetworkNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusCreated, reserved)
}

func (h *Handler) DeleteReservedIP(w http.ResponseWriter, r *http.Request) {
	networkID, err := parseIDParam(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network profile id"})
		return
	}

	reservedIDRaw := chi.URLParam(r, "reservedId")
	reservedID, err := strconv.ParseInt(reservedIDRaw, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reserved ip id"})
		return
	}

	if err := h.service.DeleteReservedIP(networkID, reservedID); err != nil {
		switch {
		case errors.Is(err, ErrReservedIPNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": reservedID, "deleted": true})
}

func parseIDParam(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	return strconv.ParseInt(raw, 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
