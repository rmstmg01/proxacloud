package topology

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"proxacloud/backend/internal/proxmox"
)

const (
	TypeSingle                       = "single"
	TypeDual                         = "dual"
	TypeTri                          = "tri"
	lowMemoryPlacementThresholdBytes = 4 * 1024 * 1024 * 1024
)

type PlanRequest struct {
	TopologyType      string `json:"topology_type"`
	ControlPlaneCount int    `json:"control_plane_count"`
	WorkerCount       int    `json:"worker_count"`
}

type Placement struct {
	Role            string `json:"role"`
	Index           int    `json:"index"`
	ProxmoxNodeName string `json:"proxmox_node_name"`
}

type PlanResponse struct {
	TopologyType        string      `json:"topology_type"`
	RequestedNodes      int         `json:"requested_nodes"`
	AvailableNodes      int         `json:"available_nodes"`
	ControlPlaneCount   int         `json:"control_plane_count"`
	WorkerCount         int         `json:"worker_count"`
	SelectedProxmoxNode []string    `json:"selected_proxmox_nodes"`
	Placements          []Placement `json:"placements"`
	Warnings            []string    `json:"warnings"`
}

type NodeInventory interface {
	ListEnabledNodes() ([]proxmox.Node, error)
}

type Service struct {
	inventory NodeInventory
}

func NewService(inventory NodeInventory) *Service {
	return &Service{inventory: inventory}
}

func (s *Service) Plan(req PlanRequest) (PlanResponse, error) {
	t := strings.ToLower(strings.TrimSpace(req.TopologyType))
	if t != TypeSingle && t != TypeDual && t != TypeTri {
		return PlanResponse{}, errors.New("topology_type must be one of single, dual, tri")
	}

	nodes, err := s.inventory.ListEnabledNodes()
	if err != nil {
		return PlanResponse{}, fmt.Errorf("list enabled proxmox nodes: %w", err)
	}
	if len(nodes) == 0 {
		return PlanResponse{}, errors.New("no enabled proxmox nodes discovered; run discovery first")
	}
	online := nodes[:0]
	for _, node := range nodes {
		if strings.TrimSpace(node.Status) == "" || strings.EqualFold(node.Status, "online") {
			online = append(online, node)
		}
	}
	nodes = online
	if len(nodes) == 0 {
		return PlanResponse{}, errors.New("no enabled online proxmox nodes are available")
	}

	minNodes := minimumNodesForTopology(t)
	if len(nodes) < minNodes {
		return PlanResponse{}, fmt.Errorf("topology %s requires at least %d enabled proxmox nodes; found %d", t, minNodes, len(nodes))
	}

	controlPlaneCount := req.ControlPlaneCount
	if controlPlaneCount <= 0 {
		switch t {
		case TypeSingle:
			controlPlaneCount = 1
		case TypeDual:
			controlPlaneCount = 2
		case TypeTri:
			controlPlaneCount = 3
		}
	}
	if controlPlaneCount < 1 {
		return PlanResponse{}, errors.New("control_plane_count must be at least 1")
	}

	workerCount := req.WorkerCount
	if workerCount <= 0 {
		workerCount = 2
	}

	sort.Slice(nodes, func(i, j int) bool {
		leftAvailable := availableMemoryBytes(nodes[i])
		rightAvailable := availableMemoryBytes(nodes[j])
		if leftAvailable == rightAvailable {
			return nodes[i].NodeName < nodes[j].NodeName
		}
		return leftAvailable > rightAvailable
	})

	selectedNodeCount := minNodes
	selected := nodes[:selectedNodeCount]

	selectedNames := make([]string, 0, len(selected))
	for _, n := range selected {
		selectedNames = append(selectedNames, n.NodeName)
	}

	placements := make([]Placement, 0, controlPlaneCount+workerCount)
	placementCountByNode := make(map[string]int, len(selected))
	nextNodeIndex := 0
	for i := 0; i < controlPlaneCount; i++ {
		nodeName, pickErr := pickPlacementNode(selected, placementCountByNode, &nextNodeIndex)
		if pickErr != nil {
			return PlanResponse{}, pickErr
		}
		placements = append(placements, Placement{
			Role:            "control-plane",
			Index:           i + 1,
			ProxmoxNodeName: nodeName,
		})
	}
	for i := 0; i < workerCount; i++ {
		nodeName, pickErr := pickPlacementNode(selected, placementCountByNode, &nextNodeIndex)
		if pickErr != nil {
			return PlanResponse{}, pickErr
		}
		placements = append(placements, Placement{
			Role:            "worker",
			Index:           i + 1,
			ProxmoxNodeName: nodeName,
		})
	}

	warnings := make([]string, 0)
	if t == TypeSingle {
		warnings = append(warnings, "single-node topology is not highly available")
	}
	if t == TypeDual {
		warnings = append(warnings, "dual-node topology has weaker control-plane quorum than tri-node")
	}
	for _, n := range selected {
		if availableMemoryBytes(n) < lowMemoryPlacementThresholdBytes {
			warnings = append(warnings, fmt.Sprintf("proxmox node %s has less than 4GB free RAM; planner limits it to a single VM placement", n.NodeName))
		}
	}

	return PlanResponse{
		TopologyType:        t,
		RequestedNodes:      minNodes,
		AvailableNodes:      len(nodes),
		ControlPlaneCount:   controlPlaneCount,
		WorkerCount:         workerCount,
		SelectedProxmoxNode: selectedNames,
		Placements:          placements,
		Warnings:            warnings,
	}, nil
}

func pickPlacementNode(selected []proxmox.Node, placementCountByNode map[string]int, nextNodeIndex *int) (string, error) {
	if len(selected) == 0 {
		return "", errors.New("no proxmox nodes selected for placement")
	}

	for i := 0; i < len(selected); i++ {
		idx := (*nextNodeIndex + i) % len(selected)
		node := selected[idx]
		count := placementCountByNode[node.NodeName]
		if availableMemoryBytes(node) < lowMemoryPlacementThresholdBytes && count >= 1 {
			continue
		}

		placementCountByNode[node.NodeName] = count + 1
		*nextNodeIndex = (idx + 1) % len(selected)
		return node.NodeName, nil
	}

	return "", fmt.Errorf("insufficient placement capacity: nodes below 4GB free RAM can host only one VM each")
}

func availableMemoryBytes(node proxmox.Node) int64 {
	available := node.MemoryTotalBytes - node.MemoryUsedBytes
	if available < 0 {
		return 0
	}
	return available
}

func minimumNodesForTopology(topologyType string) int {
	switch topologyType {
	case TypeSingle:
		return 1
	case TypeDual:
		return 2
	case TypeTri:
		return 3
	default:
		return 1
	}
}
