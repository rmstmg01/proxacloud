package clusters

import (
	"testing"

	"proxacloud/backend/internal/proxmox"
)

func TestStorageContainsVolume(t *testing.T) {
	content := []proxmox.StorageContent{
		{VolID: "cloud-images:import/ubuntu-24.04-server-cloudimg-amd64.img"},
		{VolID: "cloud-images:import/ubuntu-24.10-server-cloudimg-arm64.img"},
	}
	if !storageContainsVolume(content, "cloud-images:import/ubuntu-24.04-server-cloudimg-amd64.img") {
		t.Fatal("expected current NFS image to be found")
	}
	if storageContainsVolume(content, "cloud-images:import/ubuntu-22.04-server-cloudimg-amd64.img") {
		t.Fatal("removed NFS image was reported as present")
	}
}
