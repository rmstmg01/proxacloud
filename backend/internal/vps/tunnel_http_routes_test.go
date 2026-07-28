package vps

import "testing"

func TestValidateTunnelHTTPRoute(t *testing.T) {
	service := &Service{tunnelConfig: TunnelConfig{HostnameSuffix: "proxaops.com"}}

	name, hostname, target, tlsMode, err := service.validateTunnelHTTPRoute(
		" Merchant Admin ",
		"Merchant.Example.ProxaOps.com.",
		"http://127.0.0.1:5173",
		"",
	)
	if err != nil {
		t.Fatalf("valid route returned error: %v", err)
	}
	if name != "Merchant Admin" {
		t.Fatalf("unexpected name: %q", name)
	}
	if hostname != "merchant.example.proxaops.com" {
		t.Fatalf("unexpected hostname: %q", hostname)
	}
	if target != "http://127.0.0.1:5173" {
		t.Fatalf("unexpected target: %q", target)
	}
	if tlsMode != "edge" {
		t.Fatalf("unexpected TLS mode: %q", tlsMode)
	}
}

func TestValidateTunnelHTTPRouteRejectsUnsafeOrUnmanagedValues(t *testing.T) {
	service := &Service{tunnelConfig: TunnelConfig{HostnameSuffix: "proxaops.com"}}
	tests := []struct {
		name     string
		hostname string
		target   string
	}{
		{name: "outside managed domain", hostname: "app.example.com", target: "http://127.0.0.1:3000"},
		{name: "invalid hostname", hostname: "https://app.proxaops.com", target: "http://127.0.0.1:3000"},
		{name: "unsupported target scheme", hostname: "app.proxaops.com", target: "file:///etc/passwd"},
		{name: "target credentials", hostname: "app.proxaops.com", target: "http://user:pass@127.0.0.1:3000"},
		{name: "target fragment", hostname: "app.proxaops.com", target: "http://127.0.0.1:3000/#private"},
		{name: "invalid port", hostname: "app.proxaops.com", target: "http://127.0.0.1:70000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, _, err := service.validateTunnelHTTPRoute("App", test.hostname, test.target, "edge"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
