package proxacloudtunnelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListAndUpdateHTTPRoutes(t *testing.T) {
	var patch UpdateHTTPRouteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer api-token" {
			t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tunnels/tunnel-1/routes":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []Route{{
				ID: "route-1", TunnelID: "tunnel-1", Hostname: "app.example.com",
				TargetURL: "http://127.0.0.1:3000", TLSMode: "edge", Enabled: true,
			}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/routes/route-1":
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatalf("decode patch: %v", err)
			}
			_ = json.NewEncoder(w).Encode(Route{
				ID: "route-1", TunnelID: "tunnel-1", Hostname: patch.Hostname,
				TargetURL: patch.TargetURL, TLSMode: "edge", Enabled: true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIToken: "api-token"})
	routes, err := client.ListHTTPRoutes(context.Background(), "tunnel-1")
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].ID != "route-1" {
		t.Fatalf("unexpected routes: %#v", routes)
	}

	updated, err := client.UpdateHTTPRoute(context.Background(), "route-1", UpdateHTTPRouteRequest{
		Hostname: "admin.example.com", TargetURL: "http://127.0.0.1:5175",
	})
	if err != nil {
		t.Fatalf("update route: %v", err)
	}
	if patch.Hostname != "admin.example.com" || patch.TargetURL != "http://127.0.0.1:5175" {
		t.Fatalf("unexpected patch: %#v", patch)
	}
	if updated.Hostname != patch.Hostname {
		t.Fatalf("unexpected updated route: %#v", updated)
	}
}
