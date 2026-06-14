package firewalls

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

func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.List()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	resp, err := h.service.Get(id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.Create(req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	var req UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.Update(id, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	if err := h.service.Delete(id); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.CreateRule(id, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	ruleID, ok := parseID(w, r, "ruleId", "invalid firewall rule id")
	if !ok {
		return
	}
	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.UpdateRule(id, ruleID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	ruleID, ok := parseID(w, r, "ruleId", "invalid firewall rule id")
	if !ok {
		return
	}
	if err := h.service.DeleteRule(id, ruleID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": ruleID, "deleted": true})
}

func (h *Handler) CreateAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	var req CreateAttachmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.CreateAttachment(id, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	attachmentID, ok := parseID(w, r, "attachmentId", "invalid firewall attachment id")
	if !ok {
		return
	}
	if err := h.service.DeleteAttachment(id, attachmentID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": attachmentID, "deleted": true})
}

func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	resp, err := h.service.Apply(id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id", "invalid firewall id")
	if !ok {
		return
	}
	resp, err := h.service.ListEvents(id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound), errors.Is(err, ErrRuleNotFound), errors.Is(err, ErrAttachmentNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func parseID(w http.ResponseWriter, r *http.Request, name string, message string) (int64, bool) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
