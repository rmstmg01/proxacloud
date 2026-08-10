package manageddomains

import "testing"

func TestBuildHostname(t *testing.T) {
	runtime := Runtime{Domain: Domain{BaseDomain: "biknu.com", ReservedLabels: []string{"api", "admin"}}}
	hostname, err := BuildHostname("web-01", runtime)
	if err != nil {
		t.Fatalf("BuildHostname: %v", err)
	}
	if hostname != "web-01.biknu.com" {
		t.Fatalf("unexpected hostname %q", hostname)
	}
}

func TestBuildHostnameRejectsReservedAndInvalidLabels(t *testing.T) {
	runtime := Runtime{Domain: Domain{BaseDomain: "biknu.com", ReservedLabels: []string{"api"}}}
	for _, label := range []string{"api", "bad_label", "---"} {
		if _, err := BuildHostname(label, runtime); err == nil {
			t.Fatalf("expected %q to be rejected", label)
		}
	}
}

func TestValidateInputNormalizesDomain(t *testing.T) {
	input, err := validateInput(Input{Name: "Biknu", BaseDomain: ".BIKNU.COM.", CloudflareZoneID: "zone", DNSTarget: "gateway.example.com.", DNSRecordType: "cname", Active: true, AvailableForVPS: true})
	if err != nil {
		t.Fatalf("validateInput: %v", err)
	}
	if input.BaseDomain != "biknu.com" || input.DNSTarget != "gateway.example.com" || input.DNSRecordType != "CNAME" {
		t.Fatalf("input was not normalized: %#v", input)
	}
}
