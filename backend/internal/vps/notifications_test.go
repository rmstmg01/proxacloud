package vps

import "testing"

func TestCustomerNotificationEvents(t *testing.T) {
	for _, action := range []string{"terraform-prepare", "terraform-apply", "start", "shutdown", "restart", "root-password-reset", "hostname-update", "settings-update", "tunnel-http-route-delete"} {
		if !customerEvent(action, "success") || !customerEvent(action, "error") {
			t.Fatalf("missing event %s", action)
		}
		if customerEvent(action, "info") {
			t.Fatalf("intermediate event sent %s", action)
		}
	}
	for _, action := range []string{"console", "customer-email", "tunnel-email", "terraform-destroy"} {
		if customerEvent(action, "success") {
			t.Fatalf("unexpected/recursive notification: %s", action)
		}
	}
}
