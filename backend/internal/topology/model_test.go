package topology

import (
	"testing"

	"proxacloud/backend/internal/proxmox"
)

type testInventory struct{ nodes []proxmox.Node }

func (i testInventory) ListEnabledNodes() ([]proxmox.Node, error) { return i.nodes, nil }

func TestPlanSelectsOnlineNodesWithMostFreeRAM(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	service := NewService(testInventory{nodes: []proxmox.Node{
		{NodeName: "small", Status: "online", MemoryTotalBytes: 8 * gb, MemoryUsedBytes: 4 * gb},
		{NodeName: "offline-large", Status: "offline", MemoryTotalBytes: 64 * gb},
		{NodeName: "large", Status: "online", MemoryTotalBytes: 32 * gb, MemoryUsedBytes: 4 * gb},
	}})
	plan, err := service.Plan(PlanRequest{TopologyType: TypeSingle, ControlPlaneCount: 1, WorkerCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.SelectedProxmoxNode) != 1 || plan.SelectedProxmoxNode[0] != "large" {
		t.Fatalf("selected nodes = %v", plan.SelectedProxmoxNode)
	}
}
