package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"k8s-proxmox-manager/backend/internal/rbac"
)

type contextKey string

const contextUserKey contextKey = "auth_user"

type ContextUser struct {
	Username string
	Role     rbac.Role
}

func AuthMiddleware(tokens *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
				return
			}

			tokenRaw := strings.TrimSpace(header[len("Bearer "):])
			claims, err := tokens.Parse(tokenRaw)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
				return
			}

			ctx := context.WithValue(r.Context(), contextUserKey, ContextUser{
				Username: claims.Subject,
				Role:     claims.Role,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRoles(roles ...rbac.Role) func(http.Handler) http.Handler {
	allowed := make(map[rbac.Role]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			if _, exists := allowed[user.Role]; !exists {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func UserFromContext(ctx context.Context) (ContextUser, bool) {
	v := ctx.Value(contextUserKey)
	if v == nil {
		return ContextUser{}, false
	}
	u, ok := v.(ContextUser)
	return u, ok
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
