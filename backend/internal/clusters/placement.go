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

const placementInventoryFreshness = 15 * time.Minute

func placementNodeKey(credentialID int64, nodeName string) string {
	return fmt.Sprintf("%d:%s", credentialID, strings.ToLower(strings.TrimSpace(nodeName)))
}

func eligibleCloudImagesStorage(item proxmox.NodeStorage) bool {
	if !strings.EqualFold(strings.TrimSpace(item.StorageID), "cloud-images") || !item.Enabled || !item.Active {
		return false
	}
	// NFS is shared storage by design. Some Proxmox versions/API responses omit
	// the shared flag from node storage status even though the datastore type is
	// nfs and it is active on the node.
	return item.Shared || strings.EqualFold(strings.TrimSpace(item.Type), "nfs")
}

func (s *Service) listRegionNetworkProfileIDs(regionID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT rn.network_profile_id
		   FROM region_networks rn
		   JOIN regions r ON r.id = rn.region_id
		  WHERE rn.region_id = $1 AND r.enabled = TRUE
		  ORDER BY rn.network_profile_id ASC`,
		regionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list selected region subnets: %w", err)
	}
	defer rows.Close()
	profileIDs := make([]int64, 0)
	for rows.Next() {
		var profileID int64
		if err := rows.Scan(&profileID); err != nil {
			return nil, fmt.Errorf("read selected region subnet: %w", err)
		}
		if profileID > 0 {
			profileIDs = append(profileIDs, profileID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate selected region subnets: %w", err)
	}
	return profileIDs, nil
}

func (s *Service) planAutomaticInfrastructure(req topology.PlanRequest, regionID int64, controlPlaneRAMMB, workerRAMMB int) (topology.PlanResponse, int64, error) {
	nodes, err := s.listEnabledProvisionNodes(regionID)
	if err != nil {
		return topology.PlanResponse{}, 0, err
	}
	tags := map[string]int64{}
	regionProfileIDs := make([]int64, 0)
	if regionID > 0 {
		regionProfileIDs, err = s.listRegionNetworkProfileIDs(regionID)
		if err != nil {
			return topology.PlanResponse{}, 0, err
		}
		if len(regionProfileIDs) == 0 {
			return topology.PlanResponse{}, 0, errors.New("selected region has no subnet assigned")
		}
	} else {
		tags, err = s.networks.ListNodeTagMap()
		if err != nil {
			return topology.PlanResponse{}, 0, err
		}
	}
	storage, err := s.proxmoxSvc.ListStorageInventory()
	if err != nil {
		return topology.PlanResponse{}, 0, err
	}
	nfs := map[string]bool{}
	for _, item := range storage.Items {
		if eligibleCloudImagesStorage(item) {
			nfs[placementNodeKey(item.CredentialID, item.NodeName)] = true
		}
	}
	storageErrors := map[string]bool{}
	for _, item := range storage.Errors {
		storageErrors[placementNodeKey(item.CredentialID, item.NodeName)] = true
	}
	groups := map[int64][]proxmox.Node{}
	onlineCount := 0
	freshCount := 0
	credentialCount := 0
	storageCount := 0
	mappedCount := 0
	storageErrorCount := 0
	eligibleNodes := make([]proxmox.Node, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Status) != "" && !strings.EqualFold(node.Status, "online") {
			continue
		}
		onlineCount++
		if !node.LastDiscoveredAt.IsZero() && time.Since(node.LastDiscoveredAt) > placementInventoryFreshness {
			continue
		}
		freshCount++
		if node.CredentialID <= 0 {
			continue
		}
		credentialCount++
		key := placementNodeKey(node.CredentialID, node.NodeName)
		if storageErrors[key] {
			storageErrorCount++
		}
		if !nfs[key] {
			continue
		}
		storageCount++
		if regionID > 0 {
			eligibleNodes = append(eligibleNodes, node)
			continue
		}
		profileID := tags[node.NodeName]
		if profileID <= 0 {
			continue
		}
		mappedCount++
		groups[profileID] = append(groups[profileID], node)
	}
	if regionID > 0 {
		mappedCount = len(eligibleNodes)
		for _, profileID := range regionProfileIDs {
			groups[profileID] = append(groups[profileID], eligibleNodes...)
		}
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
		maxSameSubnet := 0
		for _, group := range groups {
			if len(group) > maxSameSubnet {
				maxSameSubnet = len(group)
			}
		}
		networkGuidance := "the same saved subnet tag on every node"
		if regionID > 0 {
			networkGuidance = "the selected region subnet assignment"
		}
		return topology.PlanResponse{}, 0, fmt.Errorf(
			"%s topology requires %d eligible Proxmox nodes on one subnet; selected region has %d enabled nodes (online: %d, fresh inventory: %d, credentials: %d, active cloud-images NFS: %d, subnet mapped: %d, maximum eligible on one subnet: %d, storage API errors: %d). Refresh node discovery and verify cloud-images and %s",
			strings.ToLower(strings.TrimSpace(req.TopologyType)), minimum, len(nodes), onlineCount, freshCount, credentialCount, storageCount, mappedCount, maxSameSubnet, storageErrorCount, networkGuidance,
		)
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
