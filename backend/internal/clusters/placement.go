package clusters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"proxacloud/backend/internal/proxmox"
	"proxacloud/backend/internal/topology"
)

type fixedNodeInventory struct{ nodes []proxmox.Node }

func (i fixedNodeInventory) ListEnabledNodes() ([]proxmox.Node, error) { return i.nodes, nil }

func (s *Service) planAutomaticInfrastructure(req topology.PlanRequest, regionID int64, controlPlaneRAMMB, workerRAMMB int) (topology.PlanResponse, int64, error) {
	nodes, err := s.listEnabledProvisionNodes(regionID)
	if err != nil {
		return topology.PlanResponse{}, 0, err
	}
	tags, err := s.networks.ListNodeTagMap()
	if err != nil {
		return topology.PlanResponse{}, 0, err
	}
	storage, err := s.proxmoxSvc.ListStorageInventory()
	if err != nil {
		return topology.PlanResponse{}, 0, err
	}
	nfs := map[string]bool{}
	for _, item := range storage.Items {
		if item.StorageID == "cloud-images" && item.Enabled && item.Active && item.Shared {
			nfs[item.NodeName] = true
		}
	}
	groups := map[int64][]proxmox.Node{}
	for _, node := range nodes {
		if strings.TrimSpace(node.Status) != "" && !strings.EqualFold(node.Status, "online") {
			continue
		}
		if !node.LastDiscoveredAt.IsZero() && time.Since(node.LastDiscoveredAt) > 15*time.Minute {
			continue
		}
		profileID := tags[node.NodeName]
		if profileID <= 0 || node.CredentialID <= 0 || !nfs[node.NodeName] {
			continue
		}
		groups[profileID] = append(groups[profileID], node)
	}
	minimum := 1
	switch strings.ToLower(strings.TrimSpace(req.TopologyType)) {
	case "dual":
		minimum = 2
	case "tri":
		minimum = 3
	}
	var selectedProfile int64
	var selected []proxmox.Node
	var selectedFree int64
	for profileID, group := range groups {
		if len(group) < minimum {
			continue
		}
		var free int64
		for _, node := range group {
			available := node.MemoryTotalBytes - node.MemoryUsedBytes
			if available > 0 {
				free += available
			}
		}
		if selectedProfile == 0 || free > selectedFree || (free == selectedFree && profileID < selectedProfile) {
			selectedProfile, selected, selectedFree = profileID, group, free
		}
	}
	if selectedProfile == 0 {
		return topology.PlanResponse{}, 0, fmt.Errorf("no subnet has %d eligible online nodes with fresh inventory, credentials, and shared cloud-images NFS storage", minimum)
	}
	cpCount := req.ControlPlaneCount
	if cpCount <= 0 {
		cpCount = minimum
	}
	wkCount := req.WorkerCount
	if wkCount <= 0 {
		wkCount = 2
	}
	requiredBytes := int64(cpCount*maxInt(controlPlaneRAMMB, 2048)+wkCount*maxInt(workerRAMMB, 2048)) * 1024 * 1024
	if selectedFree < requiredBytes {
		return topology.PlanResponse{}, 0, errors.New("eligible nodes do not have enough free RAM for the requested cluster")
	}
	planner := topology.NewService(fixedNodeInventory{nodes: selected})
	plan, err := planner.Plan(req)
	return plan, selectedProfile, err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
