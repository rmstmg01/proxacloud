package tunnelinfra

import "testing"

func TestHostnameUsesConfiguredSuffixAndSanitizesLabel(t *testing.T) {
	service := New(Config{HostnameSuffix: ".proxaops.com"}, nil)
	got, err := service.Hostname("My Cluster CP 1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "my-cluster-cp-1.proxaops.com" {
		t.Fatalf("Hostname() = %q", got)
	}
}
