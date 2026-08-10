package manageddomains

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) ListAvailable(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), true)
	if err != nil {
		respond(w, nil, err)
		return
	}
	type choice struct {
		ID             int64    `json:"id"`
		Name           string   `json:"name"`
		BaseDomain     string   `json:"base_domain"`
		ReservedLabels []string `json:"reserved_labels"`
		IsDefault      bool     `json:"is_default"`
	}
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		choices = append(choices, choice{ID: item.ID, Name: item.Name, BaseDomain: item.BaseDomain, ReservedLabels: item.ReservedLabels, IsDefault: item.IsDefault})
	}
	write(w, http.StatusOK, choices)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), false)
	respond(w, items, err)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		write(w, 400, map[string]string{"error": "invalid request payload"})
		return
	}
	item, err := h.service.Create(r.Context(), in)
	if err != nil {
		write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	write(w, 201, item)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := domainID(w, r)
	if !ok {
		return
	}
	var in Input
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		write(w, 400, map[string]string{"error": "invalid request payload"})
		return
	}
	item, err := h.service.Update(r.Context(), id, in)
	respond(w, item, err)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := domainID(w, r)
	if !ok {
		return
	}
	err := h.service.Delete(r.Context(), id)
	if errors.Is(err, ErrInUse) {
		write(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	write(w, 200, map[string]any{"id": id, "deleted": true})
}
func (h *Handler) Availability(w http.ResponseWriter, r *http.Request) {
	id, ok := domainID(w, r)
	if !ok {
		return
	}
	runtime, err := h.service.RuntimeByID(r.Context(), id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	hostname, err := BuildHostname(r.URL.Query().Get("label"), runtime)
	if err != nil {
		write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var used bool
	err = h.service.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM vps_tunnel_attachments WHERE public_hostname=$1 UNION ALL SELECT 1 FROM vps_tunnel_http_routes WHERE public_hostname=$1)`, hostname).Scan(&used)
	if err != nil {
		respond(w, nil, err)
		return
	}
	write(w, 200, map[string]any{"hostname": hostname, "available": !used})
}
func domainID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		write(w, 400, map[string]string{"error": "invalid domain id"})
		return 0, false
	}
	return id, true
}
func respond(w http.ResponseWriter, value any, err error) {
	if errors.Is(err, ErrNotFound) {
		write(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		write(w, 500, map[string]string{"error": err.Error()})
		return
	}
	write(w, 200, value)
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
