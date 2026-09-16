package vps

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

type regionTestResolver struct{}

func (regionTestResolver) TenantScope(username string, role rbac.Role) (tenant.Scope, error) {
	return tenant.Scope{Username: username, Role: role, CustomerAccountID: 1}, nil
}

// The service dependencies are intentionally absent: invalid region requests must
// be rejected before billing, placement, infrastructure calls or job creation.
func TestProvisionRegionGuard(t *testing.T) {
	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	h := NewHandler(&Service{db: db}, regionTestResolver{}, nil)
	tokens := auth.NewTokenService("region-test-secret", 10)
	token, err := tokens.Issue("alice", rbac.RoleNormalUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct {
		name    string
		handler http.HandlerFunc
		nested  bool
	}{
		{"Create", h.Create, false},
	} {
		for _, tc := range []struct {
			name, body string
			status     int
		}{
			{"omitted region with node override", `{"proxmox_credential_id":99,"proxmox_node_name":"forbidden-node","customer_account_id":999}`, 400},
			{"negative region", `{"region_id":-1}`, 400},
			{"permission lookup failure", `{"region_id":42,"customer_account_id":999}`, 500},
		} {
			t.Run(endpoint.name+"/"+tc.name, func(t *testing.T) {
				body := tc.body
				if endpoint.nested {
					body = `{"provision":` + body + `}`
				}
				req := httptest.NewRequest("POST", "/", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				auth.AuthMiddleware(tokens)(endpoint.handler).ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
