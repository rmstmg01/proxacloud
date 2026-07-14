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
		{"AlmaLinux-8-GenericCloud-latest.x86_64.qcow2", "almalinux", "8.0", "AlmaLinux 8.0", "amd64"},
		{"AlmaLinux-9-GenericCloud-9.7.x86_64.qcow2", "almalinux", "9.7", "AlmaLinux 9.7", "amd64"},
		{"AlmaLinux-10.2-GenericCloud-latest.x86_64.qcow2", "almalinux", "10.2", "AlmaLinux 10.2", "amd64"},
		{"Rocky-9-GenericCloud-Base.latest.x86_64.qcow2", "rocky", "9.0", "Rocky Linux 9.0", "amd64"},
		{"ubuntu-24.04-server-cloudimg-amd64.img", "ubuntu", "24.04", "Ubuntu 24.04", "amd64"},
		{"debian-11-genericcloud-amd64.qcow2", "debian", "11.0", "Debian 11.0", "amd64"},
		{"Fedora-Cloud-Base-Generic-42-1.1.aarch64.qcow2", "fedora", "42.0", "Fedora 42.0", "arm64"},
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

func TestIsSupportedCloudImageVolID(t *testing.T) {
	for _, volumeID := range []string{
		"cloud-images:import/ubuntu-24.04.img",
		"cloud-images:import/debian-12.qcow2",
		"cloud-images:import/fedora-42.raw",
	} {
		if !isSupportedCloudImageVolID(volumeID) {
			t.Fatalf("expected %q to be discoverable", volumeID)
		}
	}
	for _, volumeID := range []string{
		"cloud-images:iso/ubuntu-24.04.img",
		"cloud-images:import/readme.txt",
		"ubuntu-24.04.img",
	} {
		if isSupportedCloudImageVolID(volumeID) {
			t.Fatalf("expected %q to be ignored", volumeID)
		}
	}
}
