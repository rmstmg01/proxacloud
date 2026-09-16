package regions

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

func TestAccessPolicy(t *testing.T) {
	for _, tc := range []struct {
		policy  AccessPolicy
		region  int64
		allowed bool
	}{
		{AccessPolicy{Mode: "all"}, 2, true},
		{AccessPolicy{Mode: "none", RegionIDs: []int64{2}}, 2, false},
		{AccessPolicy{Mode: "selected", RegionIDs: []int64{2}}, 2, true},
		{AccessPolicy{Mode: "selected", RegionIDs: []int64{2}}, 3, false},
		{AccessPolicy{Mode: "selected"}, 2, false},
		{AccessPolicy{Mode: "unknown"}, 2, false},
	} {
		if got := tc.policy.Allows(tc.region); got != tc.allowed {
			t.Errorf("%+v region %d: %v", tc.policy, tc.region, got)
		}
	}
}

func TestDeploymentGuardFailsClosed(t *testing.T) {
	scope := tenant.Scope{Username: "alice", Role: rbac.RoleNormalUser, CustomerAccountID: 1}
	for _, id := range []int64{0, -1} {
		w := httptest.NewRecorder()
		if RequireDeployment(w, nil, scope, id) || w.Code != http.StatusBadRequest {
			t.Fatal("missing region accepted")
		}
	}
	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	w := httptest.NewRecorder()
	if RequireDeployment(w, db, scope, 1) || w.Code != 500 {
		t.Fatal("permission lookup failure must block deployment")
	}
	for _, role := range []rbac.Role{rbac.RoleSuperAdmin, rbac.RoleSuperUser} {
		if !RequireDeployment(httptest.NewRecorder(), nil, tenant.Scope{Role: role}, 0) {
			t.Fatal("admin workflow changed")
		}
	}
}

func TestRegionVisibilitySurvivesPermissionChanges(t *testing.T) {
	list := []Region{{ID: 1, Enabled: true}, {ID: 2, Enabled: true}, {ID: 3, Enabled: false}}
	annotateAccess(list, AccessPolicy{Mode: "selected", RegionIDs: []int64{2, 3}})
	if len(list) != 3 || list[0].ID != 1 || list[1].ID != 2 || list[2].ID != 3 {
		t.Fatal("region visibility changed")
	}
	if list[0].CanDeploy || list[0].DeploymentUnavailableReason != "Not permitted" || !list[1].CanDeploy || list[2].CanDeploy {
		t.Fatalf("incorrect permissions: %+v", list)
	}
	annotateAccess(list, AccessPolicy{Mode: "none"})
	if list[1].CanDeploy {
		t.Fatal("revoked permission retained")
	}
	annotateAccess(list, AccessPolicy{Mode: "all"})
	if !list[0].CanDeploy || !list[1].CanDeploy || list[2].CanDeploy || list[0].DeploymentUnavailableReason != "" {
		t.Fatalf("incorrect unrestricted permissions: %+v", list)
	}
}

func TestRegionPolicyAdminOnly(t *testing.T) {
	tokens := auth.NewTokenService("region-test-secret", 10)
	router := chi.NewRouter()
	router.Use(auth.AuthMiddleware(tokens))
	handler := NewHandler(nil) // Any access to the handler would fail the test.
	router.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Get("/users/{id}/region-access", handler.GetUserAccess)
	router.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Put("/users/{id}/region-access", handler.SetUserAccess)
	for _, role := range []rbac.Role{rbac.RoleNormalUser, rbac.RoleSuperUser} {
		token, err := tokens.Issue("alice", role)
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"GET", "PUT"} {
			req := httptest.NewRequest(method, "/users/1/region-access", strings.NewReader(`{"mode":"all"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatalf("%s %s: %d", role, method, w.Code)
			}
		}
	}
}

// Run against an isolated schema with TEST_POSTGRES_DSN; never touches application tables.
func TestRegionAccessPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("region_access_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must("SET search_path TO " + pq.QuoteIdentifier(schema))
	must(`CREATE TABLE users(id BIGSERIAL PRIMARY KEY, username TEXT);
 CREATE TABLE regions(id BIGSERIAL PRIMARY KEY, enabled BOOLEAN);
 INSERT INTO users(username) VALUES ('existing');
 INSERT INTO regions(enabled) VALUES (TRUE), (TRUE), (FALSE);`)
	migration, err := os.ReadFile("../../migrations/0076_user_region_permissions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	must(string(migration))
	must(`INSERT INTO users(username) VALUES ('new')`)
	service := NewService(db)
	existing, err := service.Policy(1)
	if err != nil || existing.Mode != "all" {
		t.Fatalf("existing access: %+v %v", existing, err)
	}
	fresh, err := service.Policy(2)
	if err != nil || fresh.Mode != "none" {
		t.Fatalf("new access: %+v %v", fresh, err)
	}
	check := func(username string, id int64, allowed bool) {
		t.Helper()
		w := httptest.NewRecorder()
		got := RequireDeployment(w, db, tenant.Scope{Username: username, Role: rbac.RoleNormalUser}, id)
		if got != allowed || (!got && w.Code != 403) {
			t.Fatalf("%s region %d: %v HTTP %d", username, id, got, w.Code)
		}
	}
	check("existing", 1, true)
	check("existing", 3, false)
	check("new", 1, false)
	if err := service.SavePolicy(2, AccessPolicy{Mode: "selected", RegionIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	check("new", 1, false)
	check("new", 2, true)
	check("missing", 2, false)
	// Re-running startup migrations must preserve an admin's choices.
	must(string(migration))
	check("new", 1, false)
	check("new", 2, true)
	for _, policy := range []AccessPolicy{
		{Mode: "invalid"}, {Mode: "none", RegionIDs: []int64{2}},
		{Mode: "selected", RegionIDs: []int64{2, 2}}, {Mode: "selected", RegionIDs: []int64{999}},
	} {
		if err := service.SavePolicy(2, policy); err == nil {
			t.Fatalf("invalid policy accepted: %+v", policy)
		}
		check("new", 2, true)
	}
	if err := service.SavePolicy(2, AccessPolicy{Mode: "none"}); err != nil {
		t.Fatal(err)
	}
	check("new", 2, false)
	if err := service.SavePolicy(2, AccessPolicy{Mode: "all"}); err != nil {
		t.Fatal(err)
	}
	must(`INSERT INTO regions(enabled) VALUES (TRUE)`)
	check("new", 4, true)
	must(`UPDATE regions SET enabled = FALSE WHERE id = 4`)
	check("new", 4, false)
}
