package topology

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"k8s-proxmox-manager/backend/internal/proxmox"
)

const (
	TypeSingle = "single"
	TypeDual   = "dual"
	TypeTri    = "tri"
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
		return nodes[i].NodeName < nodes[j].NodeName
	})

	selectedNodeCount := minNodes
	selected := nodes[:selectedNodeCount]

	selectedNames := make([]string, 0, len(selected))
	for _, n := range selected {
		selectedNames = append(selectedNames, n.NodeName)
	}

	placements := make([]Placement, 0, controlPlaneCount+workerCount)
	for i := 0; i < controlPlaneCount; i++ {
		placements = append(placements, Placement{
			Role:            "control-plane",
			Index:           i + 1,
			ProxmoxNodeName: selected[i%len(selected)].NodeName,
		})
	}
	for i := 0; i < workerCount; i++ {
		placements = append(placements, Placement{
			Role:            "worker",
			Index:           i + 1,
			ProxmoxNodeName: selected[i%len(selected)].NodeName,
		})
	}

	warnings := make([]string, 0)
	if t == TypeSingle {
		warnings = append(warnings, "single-node topology is not highly available")
	}
	if t == TypeDual {
		warnings = append(warnings, "dual-node topology has weaker control-plane quorum than tri-node")
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
