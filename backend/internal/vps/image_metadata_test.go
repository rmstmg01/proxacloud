package vps

import "testing"

func TestInferCloudImageMetadata(t *testing.T) {
	tests := []struct {
		filename     string
		distribution string
		version      string
		displayName  string
		architecture string
	}{
		{"AlmaLinux-8-GenericCloud-latest.x86_64.qcow2", "almalinux", "8", "AlmaLinux 8", "amd64"},
		{"AlmaLinux-10-GenericCloud-latest.x86_64.qcow2", "almalinux", "10", "AlmaLinux 10", "amd64"},
		{"Rocky-9-GenericCloud-Base.latest.x86_64.qcow2", "rocky", "9", "Rocky Linux 9", "amd64"},
		{"ubuntu-24.04-server-cloudimg-amd64.img", "ubuntu", "24.04", "Ubuntu 24.04", "amd64"},
		{"debian-12-genericcloud-amd64.qcow2", "debian", "12", "Debian 12", "amd64"},
		{"Fedora-Cloud-Base-Generic-42-1.1.aarch64.qcow2", "fedora", "42", "Fedora 42", "arm64"},
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			got := inferCloudImageMetadata(test.filename)
			if got.Distribution != test.distribution || got.Version != test.version || got.DisplayName != test.displayName || got.Architecture != test.architecture {
				t.Fatalf("inferCloudImageMetadata() = distribution %q, version %q, display %q, architecture %q", got.Distribution, got.Version, got.DisplayName, got.Architecture)
			}
			if got.SSHUser != "root" {
				t.Fatalf("inferCloudImageMetadata() SSH user = %q, want root", got.SSHUser)
			}
		})
	}
}
