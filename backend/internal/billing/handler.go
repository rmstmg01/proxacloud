package billing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

type Handler struct {
	service  *Service
	resolver ScopeResolver
}

func NewHandler(service *Service, resolver ScopeResolver) *Handler {
	return &Handler{service: service, resolver: resolver}
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	accountID := int64(0)
	if scope.IsAdmin() {
		accountID, _ = strconv.ParseInt(r.URL.Query().Get("account_id"), 10, 64)
	}
	resp, err := h.service.Summary(scope, accountID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListAccounts(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.ListAccounts()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListPrices(w http.ResponseWriter, _ *http.Request) {
	resp, err := h.service.ListPrices()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdatePrice(w http.ResponseWriter, r *http.Request) {
	var req Price
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.UpdatePrice(chi.URLParam(r, "resourceType"), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddCredit(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseIDParam(w, r, "accountID")
	if !ok {
		return
	}
	var req struct {
		AmountCents int    `json:"amount_cents"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	actorID := h.actorID(r)
	resp, err := h.service.AddCredit(accountID, req.AmountCents, req.Description, "admin", "", actorID, "")
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ChargeCurrentMonth(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseIDParam(w, r, "accountID")
	if !ok {
		return
	}
	resp, err := h.service.ChargeCurrentMonth(accountID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	accountID := scope.CustomerAccountID
	if scope.IsAdmin() {
		accountID, _ = strconv.ParseInt(r.URL.Query().Get("account_id"), 10, 64)
	}
	if accountID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}
	var req CheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	resp, err := h.service.CreateCheckout(accountID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) CapturePayPal(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	transactionID, ok := parseIDParam(w, r, "transactionID")
	if !ok {
		return
	}
	accountID := scope.CustomerAccountID
	if scope.IsAdmin() {
		payment, err := h.service.Payment(transactionID)
		if err != nil {
			h.writeError(w, err)
			return
		}
		accountID = payment.AccountID
	}
	resp, err := h.service.CapturePayPalOrder(accountID, transactionID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook payload"})
		return
	}
	if err := h.service.stripe.VerifyWebhook(payload, r.Header.Get("Stripe-Signature")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var event StripeWebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid stripe event"})
		return
	}
	if event.Type == "checkout.session.completed" && event.Data.Object.PaymentStatus == "paid" {
		rawID := strings.TrimSpace(event.Data.Object.Metadata["transaction_id"])
		transactionID, _ := strconv.ParseInt(rawID, 10, 64)
		if transactionID > 0 {
			if err := h.service.CompletePayment(transactionID, event.Data.Object.ID, string(payload)); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
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

func (h *Handler) actorID(r *http.Request) int64 {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		return 0
	}
	var userID int64
	_ = h.service.db.QueryRow(`SELECT id FROM users WHERE username = $1`, ctxUser.Username).Scan(&userID)
	return userID
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrTransactionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrInvalidAmount), errors.Is(err, ErrInvalidProvider):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func parseIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid " + name})
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

var _ = rbac.RoleNormalUser
