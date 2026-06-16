package users

import (
	"encoding/json"
	"net/http"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/rbac"
)

func (s *Service) RequireActiveCustomer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxUser, ok := auth.UserFromContext(r.Context())
			if !ok {
				writeGuardJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			if ctxUser.Role == rbac.RoleSuperAdmin || ctxUser.Role == rbac.RoleSuperUser {
				next.ServeHTTP(w, r)
				return
			}
			user, exists := s.GetByUsername(ctxUser.Username)
			if !exists {
				writeGuardJSON(w, http.StatusForbidden, map[string]string{"error": "customer account not found"})
				return
			}
			if user.Status != StatusActive || user.EmailVerifiedAt == nil {
				writeGuardJSON(w, http.StatusForbidden, map[string]string{"error": "account must be email verified and approved by admin before creating resources"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeGuardJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
