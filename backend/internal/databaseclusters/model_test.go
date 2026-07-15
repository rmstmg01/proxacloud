package databaseclusters

import (
	"strings"
	"testing"

	"proxacloud/backend/internal/proxmox"
)

func TestGenerateDatabasePassword(t *testing.T) {
	first, err := generateDatabasePassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateDatabasePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 24 || first == second {
		t.Fatalf("expected distinct strong passwords, got lengths %d and %d", len(first), len(second))
	}
	if strings.ContainsAny(first, "'\r\n") {
		t.Fatal("generated password contains characters unsafe for database bootstrap templates")
	}
}

func TestSelectProxmoxNodesAutomaticallyPrefersFreeRAM(t *testing.T) {
	nodes := []proxmox.Node{
		{NodeName: "pve-low", MemoryTotalBytes: 16, MemoryUsedBytes: 12},
		{NodeName: "pve-high", MemoryTotalBytes: 16, MemoryUsedBytes: 1},
		{NodeName: "pve-mid", MemoryTotalBytes: 16, MemoryUsedBytes: 5},
		{NodeName: "pve-fourth", MemoryTotalBytes: 16, MemoryUsedBytes: 8},
	}
	selected, err := selectProxmoxNodes(nodes, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 3 || selected[0].NodeName != "pve-high" || selected[1].NodeName != "pve-mid" || selected[2].NodeName != "pve-fourth" {
		t.Fatalf("unexpected automatic placement: %+v", selected)
	}
}
