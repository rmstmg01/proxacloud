package managedstorefront

import (
	"testing"
)

func TestValidateManagedStorefrontHostname(t *testing.T) {
	service := New(nil, Config{BaseDomain: "store.proxaops.com"}, nil, nil)
	valid, err := service.validateHostname("DevOpsFlare.store.proxaops.com.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != "devopsflare.store.proxaops.com" {
		t.Fatalf("hostname = %q", valid)
	}
	for _, hostname := range []string{
		"store.proxaops.com",
		"a.b.store.proxaops.com",
		"admin.proxaops.com",
		"-invalid.store.proxaops.com",
	} {
		if _, err := service.validateHostname(hostname); err == nil {
			t.Fatalf("expected %q to be rejected", hostname)
		}
	}
}

func TestValidateBiknuManagedStorefrontHostname(t *testing.T) {
	valid, err := validateHostnameForBase("DevOps.biknu.com.", "biknu.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != "devops.biknu.com" {
		t.Fatalf("hostname = %q", valid)
	}
	for _, hostname := range []string{"biknu.com", "a.b.biknu.com", "devops.store.proxaops.com"} {
		if _, err := validateHostnameForBase(hostname, "biknu.com"); err == nil {
			t.Fatalf("expected %q to be rejected", hostname)
		}
	}
}

func TestAuthorizeUsesConfiguredToken(t *testing.T) {
	service := New(nil, Config{APIToken: "expected-token"}, nil, nil)
	if !service.Authorize("expected-token") {
		t.Fatal("expected valid token")
	}
	if service.Authorize("wrong-token") {
		t.Fatal("expected invalid token")
	}
}

func TestValidateManagedStorefrontTargetURL(t *testing.T) {
	for _, target := range []string{"http://storefront:3000", "https://storefront.example.com"} {
		if err := validateTargetURL(target); err != nil {
			t.Fatalf("expected %q to be valid: %v", target, err)
		}
	}
	for _, target := range []string{"", "storefront:3000", "ftp://storefront.example.com"} {
		if err := validateTargetURL(target); err == nil {
			t.Fatalf("expected %q to be rejected", target)
		}
	}
}
