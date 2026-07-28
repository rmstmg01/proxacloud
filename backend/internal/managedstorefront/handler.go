package managedstorefront

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Ensure(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	endpoint, err := h.service.Ensure(r.Context(), chi.URLParam(r, "publicationID"), request.Hostname)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, endpoint)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	endpoint, err := h.service.Get(r.Context(), strings.TrimSpace(chi.URLParam(r, "publicationID")))
	if errors.Is(err, sql.ErrNoRows) {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "managed storefront endpoint not found"})
		return
	}
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, endpoint)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.service.List(r.Context())
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, endpoints)
}

func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Disable(r.Context(), strings.TrimSpace(chi.URLParam(r, "publicationID"))); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
