package proxmox

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

func (h *Handler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		Endpoint       string `json:"endpoint"`
		Username       string `json:"username"`
		Password       string `json:"password"`
		APITokenID     string `json:"api_token_id"`
		APITokenSecret string `json:"api_token_secret"`
		InsecureTLS    bool   `json:"insecure_tls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	created, err := h.service.CreateCredential(Credential{
		Name:           req.Name,
		Endpoint:       req.Endpoint,
		Username:       req.Username,
		Password:       req.Password,
		APITokenID:     req.APITokenID,
		APITokenSecret: req.APITokenSecret,
		InsecureTLS:    req.InsecureTLS,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) ListCredentials(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.service.ListCredentials())
}

func (h *Handler) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	credentialID, err := parseIDParam(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential id"})
		return
	}

	var req struct {
		Name           string `json:"name"`
		Endpoint       string `json:"endpoint"`
		Username       string `json:"username"`
		Password       string `json:"password"`
		APITokenID     string `json:"api_token_id"`
		APITokenSecret string `json:"api_token_secret"`
		InsecureTLS    bool   `json:"insecure_tls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	updated, err := h.service.UpdateCredential(credentialID, Credential{
		Name:           req.Name,
		Endpoint:       req.Endpoint,
		Username:       req.Username,
		Password:       req.Password,
		APITokenID:     req.APITokenID,
		APITokenSecret: req.APITokenSecret,
		InsecureTLS:    req.InsecureTLS,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	credentialID, err := parseIDParam(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential id"})
		return
	}

	if err := h.service.DeleteCredential(credentialID); err != nil {
		switch {
		case errors.Is(err, ErrCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrCredentialInUse):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": credentialID, "deleted": true})
}

func (h *Handler) DiscoverNodes(w http.ResponseWriter, r *http.Request) {
	credentialID, err := parseIDParam(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential id"})
		return
	}

	nodes, err := h.service.DiscoverAndSyncNodes(credentialID)
	if err != nil {
		switch {
		case errors.Is(err, ErrCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, nodes)
}

func (h *Handler) ListNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.service.ListNodes())
}

func (h *Handler) SetNodeEnabled(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parseIDParam(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid node id"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	if err := h.service.SetNodeEnabled(nodeID, req.Enabled); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": nodeID, "enabled": req.Enabled})
}

func parseIDParam(r *http.Request, name string) (int64, error) {
	raw := chi.URLParam(r, name)
	return strconv.ParseInt(raw, 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
