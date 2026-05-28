package monitoring

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

func (h *Handler) Status(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.Status()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetStack(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.Stack()
	if err != nil {
		if errors.Is(err, ErrStackNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpsertStack(w http.ResponseWriter, r *http.Request) {
	var req UpsertStackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.UpsertStack(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) StackManifest(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.StackManifest()
	if err != nil {
		if errors.Is(err, ErrStackNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListClusters(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.ListClusters()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateCluster(w http.ResponseWriter, r *http.Request) {
	var req CreateClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.CreateCluster(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ClusterManifest(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClusterID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.ClusterManifest(id)
	if err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SyncCluster(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClusterID(w, r)
	if !ok {
		return
	}
	resp, err := h.service.SyncCluster(id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrClusterNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteCluster(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClusterID(w, r)
	if !ok {
		return
	}
	var req DeleteClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.service.DeleteCluster(id, req.ConfirmationName); err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func parseClusterID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid monitored cluster id"})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
