package proxacloudtunnels

import (
	"encoding/json"
	"errors"
	"io"
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

func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.List()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.Create(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.Get(id)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.Deploy(id)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AttachToCluster(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	var req AttachTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.AttachToCluster(id, req)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateToken(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	var req UpdateTunnelTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.UpdateToken(id, req)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Manifest(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.Manifest(id)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	var req DeleteTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.service.Delete(id, req.ConfirmationName); err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTunnelID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.ListEvents(id)
	if err != nil {
		if errors.Is(err, ErrTunnelNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseTunnelID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid proxacloud tunnel id"})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
