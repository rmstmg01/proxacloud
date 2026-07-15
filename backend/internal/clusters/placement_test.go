package clusters

import (
	"testing"

	"proxacloud/backend/internal/proxmox"
)

func TestEligibleCloudImagesStorageAcceptsActiveNFSWithoutSharedFlag(t *testing.T) {
	item := proxmox.NodeStorage{
		StorageID: "cloud-images",
		Type:      "nfs",
		Enabled:   true,
		Active:    true,
		Shared:    false,
	}
	if !eligibleCloudImagesStorage(item) {
		t.Fatal("expected active cloud-images NFS storage to be eligible when the API omits the shared flag")
	}
}

func TestEligibleCloudImagesStorageRejectsUnavailableStorage(t *testing.T) {
	tests := []proxmox.NodeStorage{
		{StorageID: "cloud-images", Type: "nfs", Enabled: false, Active: true, Shared: true},
		{StorageID: "cloud-images", Type: "nfs", Enabled: true, Active: false, Shared: true},
		{StorageID: "local", Type: "nfs", Enabled: true, Active: true, Shared: true},
		{StorageID: "cloud-images", Type: "dir", Enabled: true, Active: true, Shared: false},
	}
	for _, item := range tests {
		if eligibleCloudImagesStorage(item) {
			t.Fatalf("expected storage to be ineligible: %+v", item)
		}
	}
}

func TestPlacementNodeKeyIncludesCredential(t *testing.T) {
	first := placementNodeKey(1, "pve01")
	second := placementNodeKey(2, "pve01")
	if first == second {
		t.Fatal("expected identical node names under different credentials to have different placement keys")
	}
	if placementNodeKey(1, " PVE01 ") != first {
		t.Fatal("expected placement node keys to normalize node names")
	}
}
