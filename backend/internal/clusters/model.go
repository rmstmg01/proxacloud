package clusters

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s-proxmox-manager/backend/internal/ansible"
	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/terraform"
	"k8s-proxmox-manager/backend/internal/topology"
)

type TopologyPlanner interface {
	Plan(req topology.PlanRequest) (topology.PlanResponse, error)
}

type NetworkStore interface {
	GetByID(id int64) (networks.Profile, error)
}

// ProxmoxCredentialStore is satisfied by *proxmox.Service and is used to look
// up stored credentials when running Terraform.
type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
}

type Service struct {
	db              *sql.DB
	networks        NetworkStore
	planner         TopologyPlanner
	terraformRunner *terraform.Runner
	ansibleRunner   *ansible.Runner
	proxmoxSvc      ProxmoxCredentialStore
	autoApply       bool
	autoBootstrap   bool
}

var ErrClusterExists = errors.New("cluster already exists")
var ErrClusterNotFound = errors.New("cluster not found")

type ProvisionRequest struct {
	Name                string `json:"name"`
	TopologyType        string `json:"topology_type"`
	ControlPlaneCount   int    `json:"control_plane_count"`
	WorkerCount         int    `json:"worker_count"`
	NetworkProfileID    int64  `json:"network_profile_id"`
	KubernetesVersion   string `json:"kubernetes_version"`
	UbuntuImage         string `json:"ubuntu_image"`
	SSHPublicKey        string `json:"ssh_public_key"`
	ControlPlaneCPU     int    `json:"control_plane_cpu"`
	ControlPlaneRAMMB   int    `json:"control_plane_ram_mb"`
	ControlPlaneDisk    int    `json:"control_plane_disk_gb"`
	WorkerCPU           int    `json:"worker_cpu"`
	WorkerRAMMB         int    `json:"worker_ram_mb"`
	WorkerDisk          int    `json:"worker_disk_gb"`
	ProxmoxCredentialID int64  `json:"proxmox_credential_id"`
	// NodeIPPoolStart is the first IP in the sequential pool to assign to VMs.
	// For example "192.168.8.100" assigns .100, .101, .102 … to each VM.
	// The subnet mask is derived from the selected network profile CIDR.
	NodeIPPoolStart string `json:"node_ip_pool_start"`
	Apply           bool   `json:"apply"`
}

type ProvisionResponse struct {
	ClusterID        int64                 `json:"cluster_id"`
	ClusterName      string                `json:"cluster_name"`
	Status           string                `json:"status"`
	Topology         topology.PlanResponse `json:"topology"`
	TerraformWorkDir string                `json:"terraform_work_dir"`
	TerraformOutput  string                `json:"terraform_output,omitempty"`
	Warnings         []string              `json:"warnings"`
}

type BootstrapRequest struct {
	SSHUser           string            `json:"ssh_user"`
	SSHPrivateKey     string            `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPath string            `json:"ssh_private_key_path,omitempty"`
	SSHKeyID          int64             `json:"ssh_key_id,omitempty"`
	SSHKeyName        string            `json:"ssh_key_name,omitempty"`
	ControlPlaneVIP   string            `json:"control_plane_vip"`
	PodCIDR           string            `json:"pod_cidr"`
	ServiceCIDR       string            `json:"service_cidr"`
	CNI               string            `json:"cni"`
	NodeIPs           map[string]string `json:"node_ips"`
	Apply             bool              `json:"apply"`
}

type BootstrapResponse struct {
	ClusterID      int64    `json:"cluster_id"`
	Status         string   `json:"status"`
	AnsibleWorkDir string   `json:"ansible_work_dir"`
	AnsibleOutput  string   `json:"ansible_output,omitempty"`
	Warnings       []string `json:"warnings"`
}

type BootstrapConfigRecord struct {
	ClusterID    int64         `json:"cluster_id"`
	SSHUser      string        `json:"ssh_user"`
	LastSSHKeyID sql.NullInt64 `json:"last_ssh_key_id"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type BootstrapSSHKeyRecord struct {
	ID        int64     `json:"id"`
	ClusterID int64     `json:"cluster_id"`
	KeyName   string    `json:"key_name"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ClusterNodeRecord struct {
	ID              int64  `json:"id"`
	Role            string `json:"role"`
	RoleIndex       int    `json:"role_index"`
	VMName          string `json:"vm_name"`
	IPAddress       string `json:"ip_address"`
	ProxmoxNodeName string `json:"proxmox_node_name"`
}

type ClusterSummary struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	TopologyType      string    `json:"topology_type"`
	KubernetesVersion string    `json:"kubernetes_version"`
	NetworkProfileID  int64     `json:"network_profile_id"`
	ControlPlaneCount int       `json:"control_plane_count"`
	WorkerCount       int       `json:"worker_count"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type clusterDeleteInfo struct {
	Name                string
	ProxmoxCredentialID sql.NullInt64
	TerraformWorkDir    string
}

type ProvisionRunRecord struct {
	ID         int64      `json:"id"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	WorkDir    string     `json:"work_dir"`
	Output     string     `json:"output"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type ScaleRequest struct {
	TargetWorkerCount int `json:"target_worker_count"`
}

type ScaleResponse struct {
	ClusterID           int64    `json:"cluster_id"`
	PreviousWorkerCount int      `json:"previous_worker_count"`
	NewWorkerCount      int      `json:"new_worker_count"`
	AddedNodes          []string `json:"added_nodes"`
	RemovedNodes        []string `json:"removed_nodes"`
	Warnings            []string `json:"warnings"`
}

type UpgradeRequest struct {
	TargetKubernetesVersion string `json:"target_kubernetes_version"`
}

type UpgradeResponse struct {
	ClusterID       int64  `json:"cluster_id"`
	PreviousVersion string `json:"previous_version"`
	TargetVersion   string `json:"target_version"`
	Status          string `json:"status"`
	Warning         string `json:"warning,omitempty"`
}

type ReplaceNodeRequest struct {
	FailedVMName       string `json:"failed_vm_name"`
	NewProxmoxNodeName string `json:"new_proxmox_node_name"`
	NewIPAddress       string `json:"new_ip_address"`
}

type ReplaceNodeResponse struct {
	ClusterID int64  `json:"cluster_id"`
	OldVMName string `json:"old_vm_name"`
	NewVMName string `json:"new_vm_name"`
	Status    string `json:"status"`
	Warning   string `json:"warning,omitempty"`
}

type RotateKubeconfigRequest struct {
	TTLHours int `json:"ttl_hours"`
}

type RotateKubeconfigResponse struct {
	ClusterID int64     `json:"cluster_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
}

type DeployObservabilityRequest struct {
	GrafanaAdminUser     string `json:"grafana_admin_user"`
	GrafanaAdminPassword string `json:"grafana_admin_password"`
	RetentionDays        int    `json:"retention_days"`
	StorageClass         string `json:"storage_class"`
	Apply                bool   `json:"apply"`
}

type DeployObservabilityResponse struct {
	ClusterID    int64    `json:"cluster_id"`
	Status       string   `json:"status"`
	DashboardURL string   `json:"dashboard_url"`
	ManifestDir  string   `json:"manifest_dir"`
	Warnings     []string `json:"warnings"`
}

type ObservabilityStatusResponse struct {
	ClusterID        int64     `json:"cluster_id"`
	GrafanaAdminUser string    `json:"grafana_admin_user"`
	RetentionDays    int       `json:"retention_days"`
	StorageClass     string    `json:"storage_class"`
	Status           string    `json:"status"`
	DashboardURL     string    `json:"dashboard_url"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateEtcdSnapshotRequest struct {
	StorageTarget string `json:"storage_target"`
	Apply         bool   `json:"apply"`
}

type CreateEtcdSnapshotResponse struct {
	ClusterID    int64     `json:"cluster_id"`
	SnapshotID   int64     `json:"snapshot_id"`
	SnapshotName string    `json:"snapshot_name"`
	SnapshotPath string    `json:"snapshot_path"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	Warning      string    `json:"warning,omitempty"`
}

type EtcdSnapshotRecord struct {
	ID            int64     `json:"id"`
	SnapshotName  string    `json:"snapshot_name"`
	StorageTarget string    `json:"storage_target"`
	SnapshotPath  string    `json:"snapshot_path"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type RestoreEtcdSnapshotRequest struct {
	SnapshotID int64 `json:"snapshot_id"`
	Apply      bool  `json:"apply"`
}

type RestoreEtcdSnapshotResponse struct {
	ClusterID  int64  `json:"cluster_id"`
	SnapshotID int64  `json:"snapshot_id"`
	Status     string `json:"status"`
	Warning    string `json:"warning,omitempty"`
}

func NewService(
	db *sql.DB,
	networks NetworkStore,
	planner TopologyPlanner,
	terraformRunner *terraform.Runner,
	ansibleRunner *ansible.Runner,
	proxmoxSvc ProxmoxCredentialStore,
	autoApply bool,
	autoBootstrap bool,
) *Service {
	return &Service{
		db:              db,
		networks:        networks,
		planner:         planner,
		terraformRunner: terraformRunner,
		ansibleRunner:   ansibleRunner,
		proxmoxSvc:      proxmoxSvc,
		autoApply:       autoApply,
		autoBootstrap:   autoBootstrap,
	}
}

func (s *Service) Provision(req ProvisionRequest) (ProvisionResponse, error) {
	if err := validateProvisionRequest(req); err != nil {
		return ProvisionResponse{}, err
	}

	networkProfile, err := s.networks.GetByID(req.NetworkProfileID)
	if err != nil {
		return ProvisionResponse{}, fmt.Errorf("load network profile: %w", err)
	}

	plan, err := s.planner.Plan(topology.PlanRequest{
		TopologyType:      req.TopologyType,
		ControlPlaneCount: req.ControlPlaneCount,
		WorkerCount:       req.WorkerCount,
	})
	if err != nil {
		return ProvisionResponse{}, err
	}

	clusterID, err := s.insertCluster(req, plan)
	if err != nil {
		return ProvisionResponse{}, err
	}

	// Build the IP iterator if a pool start was given.
	ipIter, iterErr := newIPIterator(req.NodeIPPoolStart)
	if iterErr != nil {
		return ProvisionResponse{}, iterErr
	}

	vms := make([]terraform.VMNode, 0, len(plan.Placements))
	for _, placement := range plan.Placements {
		vmName := buildVMName(req.Name, placement.Role, placement.Index)
		assignedIP := ""
		if ipIter != nil {
			ip, err := ipIter.nextAvailable()
			if err != nil {
				return ProvisionResponse{}, fmt.Errorf("ip pool exhausted for %s: %w", vmName, err)
			}
			assignedIP = ip
		}
		vms = append(vms, terraform.VMNode{
			VMName:          vmName,
			Role:            placement.Role,
			RoleIndex:       placement.Index,
			ProxmoxNodeName: placement.ProxmoxNodeName,
			CPU:             pickCPU(req, placement.Role),
			MemoryMB:        pickMemory(req, placement.Role),
			DiskGB:          pickDisk(req, placement.Role),
			IPAddress:       assignedIP,
		})

		if err := s.insertClusterNode(clusterID, placement, vmName, assignedIP); err != nil {
			return ProvisionResponse{}, err
		}
	}

	workspace, err := s.terraformRunner.PrepareWorkspace(terraform.WorkspaceSpec{
		ClusterName:       req.Name,
		KubernetesVersion: req.KubernetesVersion,
		NetworkCIDR:       networkProfile.CIDR,
		Gateway:           networkProfile.Gateway,
		DNS:               networkProfile.DNS,
		Bridge:            networkProfile.Bridge,
		VLAN:              networkProfile.VLAN,
		UbuntuImage:       req.UbuntuImage,
		SSHPublicKey:      req.SSHPublicKey,
		Nodes:             vms,
	})
	if err != nil {
		return ProvisionResponse{}, err
	}

	status := "prepared"
	terraformOutput := ""
	applyNow := req.Apply || s.autoApply
	if applyNow {
		cred, credErr := s.proxmoxSvc.GetCredentialWithSecrets(req.ProxmoxCredentialID)
		if credErr != nil {
			return ProvisionResponse{}, fmt.Errorf("load proxmox credential: %w", credErr)
		}
		auth := terraform.ProxmoxAuth{
			Endpoint:       cred.Endpoint,
			Username:       cred.Username,
			Password:       cred.Password,
			APITokenID:     cred.APITokenID,
			APITokenSecret: cred.APITokenSecret,
			InsecureTLS:    cred.InsecureTLS,
			SSHUsername:    sshUserFrom(cred),
			SSHPassword:    cred.Password,
		}
		runResult, runErr := s.terraformRunner.Apply(workspace, auth)
		terraformOutput = runResult.Output
		status = runResult.Status
		if runErr != nil {
			_ = s.insertProvisionRun(clusterID, "apply", status, workspace, terraformOutput)
			_ = s.updateClusterStatus(clusterID, "failed")
			return ProvisionResponse{}, runErr
		}
	}

	if err := s.insertProvisionRun(clusterID, "provision", status, workspace, terraformOutput); err != nil {
		return ProvisionResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return ProvisionResponse{}, err
	}

	return ProvisionResponse{
		ClusterID:        clusterID,
		ClusterName:      req.Name,
		Status:           status,
		Topology:         plan,
		TerraformWorkDir: workspace,
		TerraformOutput:  terraformOutput,
		Warnings:         plan.Warnings,
	}, nil
}

func (s *Service) Bootstrap(clusterID int64, req BootstrapRequest) (BootstrapResponse, error) {
	if s.ansibleRunner == nil {
		return BootstrapResponse{}, errors.New("ansible runner is not configured")
	}
	if clusterID <= 0 {
		return BootstrapResponse{}, errors.New("cluster id is required")
	}
	if err := validateBootstrapRequest(req); err != nil {
		return BootstrapResponse{}, err
	}

	sshPrivateKey, keyID, err := s.resolveBootstrapSSHKey(clusterID, req)
	if err != nil {
		return BootstrapResponse{}, err
	}

	if err := s.upsertBootstrapConfig(clusterID, req.SSHUser, keyID); err != nil {
		return BootstrapResponse{}, err
	}

	clusterName, kubernetesVersion, err := s.getClusterBase(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}
	if len(nodes) == 0 {
		return BootstrapResponse{}, errors.New("cluster has no nodes")
	}

	ansibleNodes := make([]ansible.Node, 0, len(nodes))
	warnings := make([]string, 0)
	for _, n := range nodes {
		ip := strings.TrimSpace(n.IPAddress)
		if override, ok := req.NodeIPs[n.VMName]; ok && strings.TrimSpace(override) != "" {
			ip = strings.TrimSpace(override)
			if err := s.updateClusterNodeIP(n.ID, ip); err != nil {
				return BootstrapResponse{}, err
			}
		}
		if net.ParseIP(ip) == nil {
			if n.Role == "control-plane" {
				return BootstrapResponse{}, fmt.Errorf("missing or invalid ip for control-plane node %s; provide node_ips map", n.VMName)
			}
			warnings = append(warnings, fmt.Sprintf("skipping worker %s due to missing/invalid ip", n.VMName))
			continue
		}
		ansibleNodes = append(ansibleNodes, ansible.Node{Name: n.VMName, IP: ip, Role: n.Role})
	}

	controlPlaneCount := 0
	for _, n := range ansibleNodes {
		if n.Role == "control-plane" {
			controlPlaneCount++
		}
	}
	if controlPlaneCount == 0 {
		return BootstrapResponse{}, errors.New("no control-plane nodes with valid IPs available for bootstrap")
	}

	if strings.TrimSpace(req.ControlPlaneVIP) == "" {
		warnings = append(warnings, "control_plane_vip is empty; kubeadm init will use primary control-plane endpoint")
	}

	workDir, err := s.ansibleRunner.PrepareWorkspace(ansible.BootstrapSpec{
		ClusterName:       clusterName,
		KubernetesVersion: kubernetesVersion,
		SSHSUser:          req.SSHUser,
		SSHPrivateKey:     sshPrivateKey,
		ControlPlaneVIP:   req.ControlPlaneVIP,
		PodCIDR:           req.PodCIDR,
		ServiceCIDR:       req.ServiceCIDR,
		CNI:               req.CNI,
		Nodes:             ansibleNodes,
	})
	if err != nil {
		return BootstrapResponse{}, err
	}

	status := "bootstrap-prepared"
	ansibleOutput := ""
	applyNow := req.Apply || s.autoBootstrap
	if applyNow {
		status = "bootstrapping"
		_ = s.insertProvisionRun(clusterID, "bootstrap", status, workDir, "")
		_ = s.updateClusterStatus(clusterID, status)

		go func() {
			runResult, runErr := s.ansibleRunner.Apply(workDir)
			runStatus := runResult.Status
			runOutput := runResult.Output
			if runErr != nil {
				_ = s.insertProvisionRun(clusterID, "bootstrap", runStatus, workDir, runOutput)
				_ = s.updateClusterStatus(clusterID, "failed")
				return
			}
			_ = s.insertProvisionRun(clusterID, "bootstrap", runStatus, workDir, runOutput)
			_ = s.updateClusterStatus(clusterID, runStatus)
		}()

		return BootstrapResponse{
			ClusterID:      clusterID,
			Status:         status,
			AnsibleWorkDir: workDir,
			Warnings:       warnings,
		}, nil
	}

	if err := s.insertProvisionRun(clusterID, "bootstrap", status, workDir, ansibleOutput); err != nil {
		return BootstrapResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return BootstrapResponse{}, err
	}

	return BootstrapResponse{
		ClusterID:      clusterID,
		Status:         status,
		AnsibleWorkDir: workDir,
		AnsibleOutput:  ansibleOutput,
		Warnings:       warnings,
	}, nil
}

func (s *Service) ListClusters() ([]ClusterSummary, error) {
	rows, err := s.db.Query(
		`SELECT id, name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, status, created_at, updated_at
		 FROM clusters
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterSummary, 0)
	for rows.Next() {
		var c ClusterSummary
		if scanErr := rows.Scan(
			&c.ID,
			&c.Name,
			&c.TopologyType,
			&c.KubernetesVersion,
			&c.NetworkProfileID,
			&c.ControlPlaneCount,
			&c.WorkerCount,
			&c.Status,
			&c.CreatedAt,
			&c.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, c)
	}

	return out, nil
}

func (s *Service) GetCluster(clusterID int64) (ClusterSummary, error) {
	return s.getClusterSummary(clusterID)
}

func (s *Service) ListClusterNodes(clusterID int64) ([]ClusterNodeRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}
	return s.getClusterNodes(clusterID)
}

func (s *Service) GetBootstrapConfig(clusterID int64) (BootstrapConfigRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return BootstrapConfigRecord{}, err
	}

	return s.getBootstrapConfig(clusterID)
}

func (s *Service) ListBootstrapSSHKeys(clusterID int64) ([]BootstrapSSHKeyRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	return s.getBootstrapSSHKeys(clusterID)
}

func (s *Service) ListRuns(clusterID int64) ([]ProvisionRunRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		`SELECT id, action, status, work_dir, output, created_at, finished_at
		 FROM provision_runs
		 WHERE cluster_id = $1
		 ORDER BY created_at DESC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cluster runs: %w", err)
	}
	defer rows.Close()

	out := make([]ProvisionRunRecord, 0)
	for rows.Next() {
		var rec ProvisionRunRecord
		if scanErr := rows.Scan(&rec.ID, &rec.Action, &rec.Status, &rec.WorkDir, &rec.Output, &rec.CreatedAt, &rec.FinishedAt); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) DeleteCluster(clusterID int64) error {
	info, err := s.getClusterDeleteInfo(clusterID)
	if err != nil {
		return err
	}

	workDir := strings.TrimSpace(info.TerraformWorkDir)
	if workDir == "" {
		workDir = s.terraformRunner.WorkspaceDir(info.Name)
	}

	if info.ProxmoxCredentialID.Valid {
		cred, credErr := s.proxmoxSvc.GetCredentialWithSecrets(info.ProxmoxCredentialID.Int64)
		if credErr != nil {
			return fmt.Errorf("load proxmox credential for delete: %w", credErr)
		}
		auth := terraform.ProxmoxAuth{
			Endpoint:       cred.Endpoint,
			Username:       cred.Username,
			Password:       cred.Password,
			APITokenID:     cred.APITokenID,
			APITokenSecret: cred.APITokenSecret,
			InsecureTLS:    cred.InsecureTLS,
			SSHUsername:    sshUserFrom(cred),
			SSHPassword:    cred.Password,
		}
		if _, statErr := os.Stat(workDir); statErr == nil {
			if _, destroyErr := s.terraformRunner.Destroy(workDir, auth); destroyErr != nil {
				return fmt.Errorf("destroy cluster infrastructure: %w", destroyErr)
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect terraform workspace: %w", statErr)
		}
	} else {
		nodes, nodeErr := s.getClusterNodes(clusterID)
		if nodeErr != nil {
			return nodeErr
		}
		if len(nodes) > 0 {
			return errors.New("cluster cannot be deleted safely because its proxmox credential metadata is missing; re-save the credential mapping or destroy the VMs manually first")
		}
	}

	res, err := s.db.Exec(`DELETE FROM clusters WHERE id = $1`, clusterID)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrClusterNotFound
	}
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
	return nil
}

func (s *Service) Scale(clusterID int64, req ScaleRequest) (ScaleResponse, error) {
	if req.TargetWorkerCount < 1 {
		return ScaleResponse{}, errors.New("target_worker_count must be at least 1")
	}

	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return ScaleResponse{}, err
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return ScaleResponse{}, err
	}

	currentWorkers := make([]ClusterNodeRecord, 0)
	for _, n := range nodes {
		if n.Role == "worker" {
			currentWorkers = append(currentWorkers, n)
		}
	}

	response := ScaleResponse{
		ClusterID:           clusterID,
		PreviousWorkerCount: len(currentWorkers),
		NewWorkerCount:      req.TargetWorkerCount,
		AddedNodes:          []string{},
		RemovedNodes:        []string{},
		Warnings:            []string{},
	}

	if req.TargetWorkerCount == len(currentWorkers) {
		response.Warnings = append(response.Warnings, "target worker count equals current count; no changes applied")
		return response, nil
	}

	if req.TargetWorkerCount > len(currentWorkers) {
		nextIndex := 1
		for _, n := range currentWorkers {
			if n.RoleIndex >= nextIndex {
				nextIndex = n.RoleIndex + 1
			}
		}

		nodePool, err := s.listDistinctProxmoxNodes(clusterID)
		if err != nil {
			return ScaleResponse{}, err
		}
		if len(nodePool) == 0 {
			return ScaleResponse{}, errors.New("no proxmox nodes associated with cluster")
		}

		toAdd := req.TargetWorkerCount - len(currentWorkers)
		for i := 0; i < toAdd; i++ {
			vmName := buildVMName(cluster.Name, "worker", nextIndex)
			selectedNode := nodePool[i%len(nodePool)]
			_, execErr := s.db.Exec(
				`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name)
				 VALUES ($1, 'worker', $2, $3, $4)`,
				clusterID,
				nextIndex,
				selectedNode,
				vmName,
			)
			if execErr != nil {
				return ScaleResponse{}, fmt.Errorf("insert scaled worker node: %w", execErr)
			}
			response.AddedNodes = append(response.AddedNodes, vmName)
			nextIndex++
		}
	} else {
		toRemove := len(currentWorkers) - req.TargetWorkerCount
		for i := len(currentWorkers) - 1; i >= 0 && toRemove > 0; i-- {
			n := currentWorkers[i]
			_, execErr := s.db.Exec(`DELETE FROM cluster_nodes WHERE id = $1`, n.ID)
			if execErr != nil {
				return ScaleResponse{}, fmt.Errorf("delete worker node: %w", execErr)
			}
			response.RemovedNodes = append(response.RemovedNodes, n.VMName)
			toRemove--
		}
		response.Warnings = append(response.Warnings, "worker node records removed; VM deletion must be handled by provisioning workflow")
	}

	if _, err := s.db.Exec(
		`UPDATE clusters SET worker_count = $2, status = 'scaled', updated_at = NOW() WHERE id = $1`,
		clusterID,
		req.TargetWorkerCount,
	); err != nil {
		return ScaleResponse{}, fmt.Errorf("update cluster worker count: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "scale", "scaled", "", strings.Join(append(response.AddedNodes, response.RemovedNodes...), ",")); err != nil {
		return ScaleResponse{}, err
	}

	return response, nil
}

func (s *Service) Upgrade(clusterID int64, req UpgradeRequest) (UpgradeResponse, error) {
	target := strings.TrimSpace(req.TargetKubernetesVersion)
	if target == "" {
		return UpgradeResponse{}, errors.New("target_kubernetes_version is required")
	}

	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return UpgradeResponse{}, err
	}

	if _, err := s.db.Exec(
		`UPDATE clusters SET kubernetes_version = $2, status = 'upgrade-planned', updated_at = NOW() WHERE id = $1`,
		clusterID,
		target,
	); err != nil {
		return UpgradeResponse{}, fmt.Errorf("update cluster version: %w", err)
	}

	out := fmt.Sprintf("upgrade planned from %s to %s", cluster.KubernetesVersion, target)
	if err := s.insertProvisionRun(clusterID, "upgrade", "upgrade-planned", "", out); err != nil {
		return UpgradeResponse{}, err
	}

	return UpgradeResponse{
		ClusterID:       clusterID,
		PreviousVersion: cluster.KubernetesVersion,
		TargetVersion:   target,
		Status:          "upgrade-planned",
		Warning:         "version metadata updated; execute upgrade automation runbook before production cutover",
	}, nil
}

func (s *Service) ReplaceNode(clusterID int64, req ReplaceNodeRequest) (ReplaceNodeResponse, error) {
	failedVM := strings.TrimSpace(req.FailedVMName)
	newNode := strings.TrimSpace(req.NewProxmoxNodeName)
	newIP := strings.TrimSpace(req.NewIPAddress)
	if failedVM == "" {
		return ReplaceNodeResponse{}, errors.New("failed_vm_name is required")
	}
	if newNode == "" {
		return ReplaceNodeResponse{}, errors.New("new_proxmox_node_name is required")
	}
	if newIP != "" && net.ParseIP(newIP) == nil {
		return ReplaceNodeResponse{}, errors.New("new_ip_address is invalid")
	}

	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ReplaceNodeResponse{}, err
	}

	var existing ClusterNodeRecord
	err := s.db.QueryRow(
		`SELECT id, role, role_index, vm_name, COALESCE(ip_address, ''), proxmox_node_name
		 FROM cluster_nodes
		 WHERE cluster_id = $1 AND vm_name = $2`,
		clusterID,
		failedVM,
	).Scan(&existing.ID, &existing.Role, &existing.RoleIndex, &existing.VMName, &existing.IPAddress, &existing.ProxmoxNodeName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReplaceNodeResponse{}, errors.New("failed node not found")
		}
		return ReplaceNodeResponse{}, fmt.Errorf("query failed node: %w", err)
	}

	if existing.Role != "worker" {
		return ReplaceNodeResponse{}, errors.New("replace-node currently supports worker role only")
	}

	newVMName := existing.VMName + "-r"
	tx, err := s.db.Begin()
	if err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("begin replace node transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM cluster_nodes WHERE id = $1`, existing.ID); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("delete failed node record: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name, ip_address)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		clusterID,
		existing.Role,
		existing.RoleIndex,
		newNode,
		newVMName,
		newIP,
	); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("insert replacement node: %w", err)
	}

	if _, err := tx.Exec(`UPDATE clusters SET status = 'node-replaced', updated_at = NOW() WHERE id = $1`, clusterID); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("update cluster status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("commit replace node transaction: %w", err)
	}

	_ = s.insertProvisionRun(clusterID, "replace-node", "node-replaced", "", fmt.Sprintf("%s->%s", failedVM, newVMName))

	return ReplaceNodeResponse{
		ClusterID: clusterID,
		OldVMName: failedVM,
		NewVMName: newVMName,
		Status:    "node-replaced",
		Warning:   "replacement updates metadata; provision/decommission VM actions should be executed in infra workflow",
	}, nil
}

func (s *Service) RotateKubeconfig(clusterID int64, req RotateKubeconfigRequest) (RotateKubeconfigResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return RotateKubeconfigResponse{}, err
	}

	ttl := req.TTLHours
	if ttl <= 0 {
		ttl = 24
	}
	if ttl > 24*30 {
		return RotateKubeconfigResponse{}, errors.New("ttl_hours too large; max 720")
	}

	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return RotateKubeconfigResponse{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().UTC().Add(time.Duration(ttl) * time.Hour)

	if _, err := s.db.Exec(
		`INSERT INTO kubeconfig_tokens (cluster_id, token, expires_at, revoked)
		 VALUES ($1, $2, $3, FALSE)`,
		clusterID,
		token,
		expiresAt,
	); err != nil {
		return RotateKubeconfigResponse{}, fmt.Errorf("insert kubeconfig token: %w", err)
	}

	_ = s.insertProvisionRun(clusterID, "rotate-kubeconfig", "token-rotated", "", fmt.Sprintf("expires_at=%s", expiresAt.Format(time.RFC3339)))

	return RotateKubeconfigResponse{
		ClusterID: clusterID,
		Token:     token,
		ExpiresAt: expiresAt,
		Status:    "token-rotated",
	}, nil
}

func (s *Service) DeployObservability(clusterID int64, req DeployObservabilityRequest) (DeployObservabilityResponse, error) {
	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return DeployObservabilityResponse{}, err
	}

	adminUser := strings.TrimSpace(req.GrafanaAdminUser)
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPassword := strings.TrimSpace(req.GrafanaAdminPassword)
	if adminPassword == "" {
		return DeployObservabilityResponse{}, errors.New("grafana_admin_password is required")
	}
	retention := req.RetentionDays
	if retention <= 0 {
		retention = 15
	}
	storageClass := strings.TrimSpace(req.StorageClass)
	if storageClass == "" {
		storageClass = "default"
	}

	manifestDir := filepath.Join("infra", "operations", "runs", sanitizePathToken(cluster.Name), "observability")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("create observability workspace: %w", err)
	}

	manifest := buildObservabilityManifest(adminUser, adminPassword, retention, storageClass)
	manifestPath := filepath.Join(manifestDir, "kube-prometheus-stack-values.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("write observability manifest: %w", err)
	}

	status := "observability-prepared"
	warnings := []string{}
	if req.Apply {
		status = "observability-apply-requested"
		warnings = append(warnings, "runtime helm deployment is not executed yet; apply using generated values file in your deployment pipeline")
	}

	dashboardURL := fmt.Sprintf("http://%s-grafana.local", sanitizePathToken(cluster.Name))
	_, err = s.db.Exec(
		`INSERT INTO observability_configs (cluster_id, grafana_admin_user, grafana_admin_password, retention_days, storage_class, status, dashboard_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (cluster_id)
		 DO UPDATE SET
		   grafana_admin_user = EXCLUDED.grafana_admin_user,
		   grafana_admin_password = EXCLUDED.grafana_admin_password,
		   retention_days = EXCLUDED.retention_days,
		   storage_class = EXCLUDED.storage_class,
		   status = EXCLUDED.status,
		   dashboard_url = EXCLUDED.dashboard_url,
		   updated_at = NOW()`,
		clusterID,
		adminUser,
		adminPassword,
		retention,
		storageClass,
		status,
		dashboardURL,
	)
	if err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("upsert observability config: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "observability-deploy", status, manifestDir, manifestPath); err != nil {
		return DeployObservabilityResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return DeployObservabilityResponse{}, err
	}

	return DeployObservabilityResponse{
		ClusterID:    clusterID,
		Status:       status,
		DashboardURL: dashboardURL,
		ManifestDir:  manifestDir,
		Warnings:     warnings,
	}, nil
}

func (s *Service) GetObservability(clusterID int64) (ObservabilityStatusResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ObservabilityStatusResponse{}, err
	}

	var out ObservabilityStatusResponse
	err := s.db.QueryRow(
		`SELECT cluster_id, grafana_admin_user, retention_days, storage_class, status, COALESCE(dashboard_url, ''), updated_at
		 FROM observability_configs
		 WHERE cluster_id = $1`,
		clusterID,
	).Scan(&out.ClusterID, &out.GrafanaAdminUser, &out.RetentionDays, &out.StorageClass, &out.Status, &out.DashboardURL, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ObservabilityStatusResponse{}, errors.New("observability not configured for this cluster")
		}
		return ObservabilityStatusResponse{}, fmt.Errorf("get observability config: %w", err)
	}

	return out, nil
}

func (s *Service) CreateEtcdSnapshot(clusterID int64, req CreateEtcdSnapshotRequest) (CreateEtcdSnapshotResponse, error) {
	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}

	storageTarget := strings.TrimSpace(req.StorageTarget)
	if storageTarget == "" {
		storageTarget = filepath.Join("backups", "etcd")
	}

	snapshotName := fmt.Sprintf("%s-etcd-%s.db", sanitizePathToken(cluster.Name), time.Now().UTC().Format("20060102-150405"))
	snapshotPath := filepath.Join(storageTarget, snapshotName)
	status := "snapshot-planned"
	warning := "snapshot record prepared; execute etcdctl snapshot save from control-plane node to materialize backup"
	if req.Apply {
		status = "snapshot-requested"
	}

	var snapshotID int64
	var createdAt time.Time
	err = s.db.QueryRow(
		`INSERT INTO etcd_snapshots (cluster_id, snapshot_name, storage_target, snapshot_path, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		clusterID,
		snapshotName,
		storageTarget,
		snapshotPath,
		status,
	).Scan(&snapshotID, &createdAt)
	if err != nil {
		return CreateEtcdSnapshotResponse{}, fmt.Errorf("insert etcd snapshot record: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "etcd-snapshot", status, storageTarget, snapshotPath); err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}

	return CreateEtcdSnapshotResponse{
		ClusterID:    clusterID,
		SnapshotID:   snapshotID,
		SnapshotName: snapshotName,
		SnapshotPath: snapshotPath,
		Status:       status,
		CreatedAt:    createdAt,
		Warning:      warning,
	}, nil
}

func (s *Service) ListEtcdSnapshots(clusterID int64) ([]EtcdSnapshotRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		`SELECT id, snapshot_name, storage_target, snapshot_path, status, created_at
		 FROM etcd_snapshots
		 WHERE cluster_id = $1
		 ORDER BY created_at DESC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list etcd snapshots: %w", err)
	}
	defer rows.Close()

	out := make([]EtcdSnapshotRecord, 0)
	for rows.Next() {
		var r EtcdSnapshotRecord
		if scanErr := rows.Scan(&r.ID, &r.SnapshotName, &r.StorageTarget, &r.SnapshotPath, &r.Status, &r.CreatedAt); scanErr != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) RestoreEtcdSnapshot(clusterID int64, req RestoreEtcdSnapshotRequest) (RestoreEtcdSnapshotResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}
	if req.SnapshotID <= 0 {
		return RestoreEtcdSnapshotResponse{}, errors.New("snapshot_id is required")
	}

	var snapshotPath string
	err := s.db.QueryRow(
		`SELECT snapshot_path FROM etcd_snapshots WHERE id = $1 AND cluster_id = $2`,
		req.SnapshotID,
		clusterID,
	).Scan(&snapshotPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RestoreEtcdSnapshotResponse{}, errors.New("snapshot not found")
		}
		return RestoreEtcdSnapshotResponse{}, fmt.Errorf("query snapshot: %w", err)
	}

	status := "restore-planned"
	warning := "restore plan recorded; execute etcd restore runbook with maintenance window"
	if req.Apply {
		status = "restore-requested"
	}

	if _, err := s.db.Exec(
		`INSERT INTO etcd_restore_runs (cluster_id, snapshot_id, status, details)
		 VALUES ($1, $2, $3, $4)`,
		clusterID,
		req.SnapshotID,
		status,
		snapshotPath,
	); err != nil {
		return RestoreEtcdSnapshotResponse{}, fmt.Errorf("insert restore run: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "etcd-restore", status, "", snapshotPath); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}

	return RestoreEtcdSnapshotResponse{
		ClusterID:  clusterID,
		SnapshotID: req.SnapshotID,
		Status:     status,
		Warning:    warning,
	}, nil
}

// sshUserFrom derives the SSH system username from a Proxmox credential.
// API usernames are like "root@pam" or "user@realm"; for SSH we just want "root" / "user".
// For API-token-only credentials the token ID format is "user@realm!token-name".
func sshUserFrom(cred proxmox.Credential) string {
	if cred.Username != "" {
		return strings.SplitN(cred.Username, "@", 2)[0]
	}
	if cred.APITokenID != "" {
		return strings.SplitN(cred.APITokenID, "@", 2)[0]
	}
	return ""
}

func validateProvisionRequest(req ProvisionRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.KubernetesVersion = strings.TrimSpace(req.KubernetesVersion)
	req.UbuntuImage = strings.TrimSpace(req.UbuntuImage)
	req.SSHPublicKey = strings.TrimSpace(req.SSHPublicKey)

	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.NetworkProfileID <= 0 {
		return errors.New("network_profile_id is required")
	}
	if req.KubernetesVersion == "" {
		return errors.New("kubernetes_version is required")
	}
	if req.UbuntuImage == "" {
		return errors.New("ubuntu_image is required")
	}
	if req.SSHPublicKey == "" {
		return errors.New("ssh_public_key is required")
	}
	if req.ProxmoxCredentialID <= 0 {
		return errors.New("proxmox_credential_id is required")
	}
	if strings.TrimSpace(req.NodeIPPoolStart) == "" {
		return errors.New("node_ip_pool_start is required (e.g. 192.168.8.100)")
	}
	if net.ParseIP(strings.TrimSpace(req.NodeIPPoolStart)) == nil {
		return errors.New("node_ip_pool_start is not a valid IP address")
	}

	return nil
}

// ipIterator hands out sequential IPs starting from a base address,
// skipping any that are already responding on the network.
type ipIterator struct {
	current net.IP
}

func newIPIterator(start string) (*ipIterator, error) {
	start = strings.TrimSpace(start)
	if start == "" {
		return nil, nil
	}
	ip := net.ParseIP(start).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid node_ip_pool_start %q", start)
	}
	clone := make(net.IP, 4)
	copy(clone, ip)
	return &ipIterator{current: clone}, nil
}

// nextAvailable advances until it finds an IP that does not respond to ping,
// then returns that IP. It tries up to 64 addresses before giving up.
func (it *ipIterator) nextAvailable() (string, error) {
	const maxSkip = 64
	for i := 0; i < maxSkip; i++ {
		ip := it.advance()
		if !isIPInUse(ip) {
			return ip, nil
		}
	}
	return "", errors.New("no free IP found in the next 64 addresses of the pool")
}

// advance returns the current IP and increments to the next one.
func (it *ipIterator) advance() string {
	addr := make(net.IP, 4)
	copy(addr, it.current)
	result := addr.String()
	for i := 3; i >= 0; i-- {
		it.current[i]++
		if it.current[i] != 0 {
			break
		}
	}
	return result
}

// isIPInUse probes the given IP address with a single ICMP ping (1 s timeout).
// Returns true if the host is alive (IP already taken).
func isIPInUse(ip string) bool {
	// Try ICMP ping first — works on Linux hosts/containers with NET_RAW or
	// when run as root (as is typical in Docker containers).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", ip)
	if err := cmd.Run(); err == nil {
		return true // got a reply — IP is in use
	}

	// Fallback: try a short TCP dial to port 22 (SSH).
	// "connection refused" means the host is up; timeout means it is down.
	conn, err := net.DialTimeout("tcp", ip+":22", 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}
	// If the error is not a timeout, the host likely sent a TCP RST → it is up.
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return false
	}
	// "connection refused" or similar → host is up
	if strings.Contains(err.Error(), "refused") {
		return true
	}
	return false
}

func validateBootstrapRequest(req BootstrapRequest) error {
	if strings.TrimSpace(req.SSHUser) == "" {
		return errors.New("ssh_user is required")
	}

	if strings.TrimSpace(req.PodCIDR) != "" {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(req.PodCIDR)); err != nil {
			return errors.New("pod_cidr is invalid")
		}
	}
	if strings.TrimSpace(req.ServiceCIDR) != "" {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(req.ServiceCIDR)); err != nil {
			return errors.New("service_cidr is invalid")
		}
	}

	return nil
}

func (s *Service) insertCluster(req ProvisionRequest, plan topology.PlanResponse) (int64, error) {
	var clusterID int64
	err := s.db.QueryRow(
		`INSERT INTO clusters (name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, proxmox_credential_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'planned')
		 RETURNING id`,
		req.Name,
		plan.TopologyType,
		req.KubernetesVersion,
		req.NetworkProfileID,
		plan.ControlPlaneCount,
		plan.WorkerCount,
		req.ProxmoxCredentialID,
	).Scan(&clusterID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return 0, ErrClusterExists
		}
		return 0, fmt.Errorf("insert cluster: %w", err)
	}

	return clusterID, nil
}

func (s *Service) insertClusterNode(clusterID int64, placement topology.Placement, vmName string, ipAddress string) error {
	_, err := s.db.Exec(
		`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name, ip_address)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		clusterID,
		placement.Role,
		placement.Index,
		placement.ProxmoxNodeName,
		vmName,
		strings.TrimSpace(ipAddress),
	)
	if err != nil {
		return fmt.Errorf("insert cluster node: %w", err)
	}
	return nil
}

func (s *Service) insertProvisionRun(clusterID int64, action, status, workDir, output string) error {
	_, err := s.db.Exec(
		`INSERT INTO provision_runs (cluster_id, action, status, work_dir, output, finished_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		clusterID,
		action,
		status,
		workDir,
		output,
	)
	if err != nil {
		return fmt.Errorf("insert provision run: %w", err)
	}
	return nil
}

func (s *Service) updateClusterStatus(clusterID int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE clusters SET status = $2, updated_at = NOW() WHERE id = $1`,
		clusterID,
		status,
	)
	if err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}
	return nil
}

func (s *Service) getClusterBase(clusterID int64) (string, string, error) {
	var name string
	var kubernetesVersion string
	err := s.db.QueryRow(
		`SELECT name, kubernetes_version FROM clusters WHERE id = $1`,
		clusterID,
	).Scan(&name, &kubernetesVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrClusterNotFound
		}
		return "", "", fmt.Errorf("query cluster: %w", err)
	}
	return name, kubernetesVersion, nil
}

func (s *Service) getClusterDeleteInfo(clusterID int64) (clusterDeleteInfo, error) {
	var info clusterDeleteInfo
	err := s.db.QueryRow(
		`SELECT c.name, c.proxmox_credential_id,
		        COALESCE((
		          SELECT pr.work_dir
		          FROM provision_runs pr
		          WHERE pr.cluster_id = c.id AND pr.work_dir <> '' AND pr.action = 'provision'
		          ORDER BY pr.created_at DESC
		          LIMIT 1
		        ), '')
		 FROM clusters c
		 WHERE c.id = $1`,
		clusterID,
	).Scan(&info.Name, &info.ProxmoxCredentialID, &info.TerraformWorkDir)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clusterDeleteInfo{}, ErrClusterNotFound
		}
		return clusterDeleteInfo{}, fmt.Errorf("query cluster delete info: %w", err)
	}
	return info, nil
}

func (s *Service) getClusterNodes(clusterID int64) ([]ClusterNodeRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, role, role_index, vm_name, COALESCE(ip_address, ''), proxmox_node_name
		 FROM cluster_nodes
		 WHERE cluster_id = $1
		 ORDER BY role, role_index`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("query cluster nodes: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterNodeRecord, 0)
	for rows.Next() {
		var n ClusterNodeRecord
		if scanErr := rows.Scan(&n.ID, &n.Role, &n.RoleIndex, &n.VMName, &n.IPAddress, &n.ProxmoxNodeName); scanErr != nil {
			continue
		}
		out = append(out, n)
	}

	return out, nil
}

func (s *Service) getClusterSummary(clusterID int64) (ClusterSummary, error) {
	var c ClusterSummary
	err := s.db.QueryRow(
		`SELECT id, name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, status, created_at, updated_at
		 FROM clusters WHERE id = $1`,
		clusterID,
	).Scan(
		&c.ID,
		&c.Name,
		&c.TopologyType,
		&c.KubernetesVersion,
		&c.NetworkProfileID,
		&c.ControlPlaneCount,
		&c.WorkerCount,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClusterSummary{}, ErrClusterNotFound
		}
		return ClusterSummary{}, fmt.Errorf("get cluster summary: %w", err)
	}
	return c, nil
}

func (s *Service) getBootstrapConfig(clusterID int64) (BootstrapConfigRecord, error) {
	var out BootstrapConfigRecord
	err := s.db.QueryRow(
		`SELECT cluster_id, ssh_user, last_ssh_key_id, updated_at
		 FROM cluster_bootstrap_configs
		 WHERE cluster_id = $1`,
		clusterID,
	).Scan(&out.ClusterID, &out.SSHUser, &out.LastSSHKeyID, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BootstrapConfigRecord{}, errors.New("bootstrap ssh config not found")
		}
		return BootstrapConfigRecord{}, fmt.Errorf("query bootstrap ssh config: %w", err)
	}

	return out, nil
}

func (s *Service) getBootstrapSSHKeys(clusterID int64) ([]BootstrapSSHKeyRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, cluster_id, key_name, updated_at
		 FROM cluster_bootstrap_ssh_keys
		 WHERE cluster_id = $1
		 ORDER BY key_name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list bootstrap ssh keys: %w", err)
	}
	defer rows.Close()

	out := make([]BootstrapSSHKeyRecord, 0)
	for rows.Next() {
		var rec BootstrapSSHKeyRecord
		if scanErr := rows.Scan(&rec.ID, &rec.ClusterID, &rec.KeyName, &rec.UpdatedAt); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}

	return out, nil
}

func (s *Service) upsertBootstrapConfig(clusterID int64, sshUser string, lastSSHKeyID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO cluster_bootstrap_configs (cluster_id, ssh_user, ssh_private_key_path, last_ssh_key_id)
		 VALUES ($1, $2, '', $3)
		 ON CONFLICT (cluster_id)
		 DO UPDATE SET
		   ssh_user = EXCLUDED.ssh_user,
		   ssh_private_key_path = EXCLUDED.ssh_private_key_path,
		   last_ssh_key_id = EXCLUDED.last_ssh_key_id,
		   updated_at = NOW()`,
		clusterID,
		strings.TrimSpace(sshUser),
		lastSSHKeyID,
	)
	if err != nil {
		return fmt.Errorf("upsert bootstrap ssh config: %w", err)
	}

	return nil
}

func (s *Service) resolveBootstrapSSHKey(clusterID int64, req BootstrapRequest) (string, int64, error) {
	if key := strings.TrimSpace(req.SSHPrivateKey); key != "" {
		name := strings.TrimSpace(req.SSHKeyName)
		if name == "" {
			name = "default"
		}
		keyID, err := s.upsertBootstrapSSHKey(clusterID, name, key)
		if err != nil {
			return "", 0, err
		}
		return key, keyID, nil
	}

	if req.SSHKeyID > 0 {
		key, err := s.getBootstrapSSHKeyByID(clusterID, req.SSHKeyID)
		if err != nil {
			return "", 0, err
		}
		return key, req.SSHKeyID, nil
	}

	cfg, err := s.getBootstrapConfig(clusterID)
	if err == nil && cfg.LastSSHKeyID.Valid {
		key, keyErr := s.getBootstrapSSHKeyByID(clusterID, cfg.LastSSHKeyID.Int64)
		if keyErr != nil {
			return "", 0, keyErr
		}
		return key, cfg.LastSSHKeyID.Int64, nil
	}

	if path := strings.TrimSpace(req.SSHPrivateKeyPath); path != "" {
		bytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", 0, fmt.Errorf("read ssh private key path: %w", readErr)
		}
		key := strings.TrimSpace(string(bytes))
		if key == "" {
			return "", 0, errors.New("ssh private key file is empty")
		}
		keyID, err := s.upsertBootstrapSSHKey(clusterID, "legacy-path-import", key)
		if err != nil {
			return "", 0, err
		}
		return key, keyID, nil
	}

	return "", 0, errors.New("provide ssh_private_key or select an existing ssh_key_id")
}

func (s *Service) upsertBootstrapSSHKey(clusterID int64, keyName, privateKey string) (int64, error) {
	var keyID int64
	err := s.db.QueryRow(
		`INSERT INTO cluster_bootstrap_ssh_keys (cluster_id, key_name, private_key)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (cluster_id, key_name)
		 DO UPDATE SET
		   private_key = EXCLUDED.private_key,
		   updated_at = NOW()
		 RETURNING id`,
		clusterID,
		strings.TrimSpace(keyName),
		strings.TrimSpace(privateKey),
	).Scan(&keyID)
	if err != nil {
		return 0, fmt.Errorf("upsert bootstrap ssh key: %w", err)
	}

	return keyID, nil
}

func (s *Service) getBootstrapSSHKeyByID(clusterID, keyID int64) (string, error) {
	var privateKey string
	err := s.db.QueryRow(
		`SELECT private_key
		 FROM cluster_bootstrap_ssh_keys
		 WHERE id = $1 AND cluster_id = $2`,
		keyID,
		clusterID,
	).Scan(&privateKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("selected bootstrap ssh key not found")
		}
		return "", fmt.Errorf("query bootstrap ssh key: %w", err)
	}

	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return "", errors.New("selected bootstrap ssh key is empty")
	}

	return privateKey, nil
}

func (s *Service) ClusterNameExists(name string) (bool, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, errors.New("name is required")
	}

	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM clusters WHERE LOWER(name) = LOWER($1))`,
		trimmed,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check cluster name: %w", err)
	}

	return exists, nil
}

func (s *Service) listDistinctProxmoxNodes(clusterID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT proxmox_node_name
		 FROM cluster_nodes
		 WHERE cluster_id = $1
		 ORDER BY proxmox_node_name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list distinct proxmox nodes: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var n string
		if scanErr := rows.Scan(&n); scanErr != nil {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) updateClusterNodeIP(nodeID int64, ip string) error {
	_, err := s.db.Exec(`UPDATE cluster_nodes SET ip_address = $2 WHERE id = $1`, nodeID, ip)
	if err != nil {
		return fmt.Errorf("update cluster node ip: %w", err)
	}
	return nil
}

func pickCPU(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneCPU > 0 {
			return req.ControlPlaneCPU
		}
		return 4
	}
	if req.WorkerCPU > 0 {
		return req.WorkerCPU
	}
	return 4
}

func pickMemory(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneRAMMB > 0 {
			return req.ControlPlaneRAMMB
		}
		return 8192
	}
	if req.WorkerRAMMB > 0 {
		return req.WorkerRAMMB
	}
	return 8192
}

func pickDisk(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneDisk > 0 {
			return req.ControlPlaneDisk
		}
		return 80
	}
	if req.WorkerDisk > 0 {
		return req.WorkerDisk
	}
	return 120
}

func buildVMName(clusterName, role string, index int) string {
	roleToken := "wk"
	if role == "control-plane" {
		roleToken = "cp"
	}
	name := strings.ToLower(strings.TrimSpace(clusterName))
	name = strings.ReplaceAll(name, " ", "-")
	return fmt.Sprintf("%s-%s-%d", name, roleToken, index)
}

func sanitizePathToken(name string) string {
	token := strings.ToLower(strings.TrimSpace(name))
	token = strings.ReplaceAll(token, " ", "-")
	token = strings.ReplaceAll(token, "/", "-")
	token = strings.ReplaceAll(token, "\\", "-")
	token = strings.ReplaceAll(token, ":", "-")
	if token == "" {
		return "cluster"
	}
	return token
}

func buildObservabilityManifest(adminUser, adminPassword string, retentionDays int, storageClass string) string {
	return fmt.Sprintf(`grafana:
	adminUser: %s
	adminPassword: %s
	persistence:
		enabled: true
		storageClassName: %s
		size: 10Gi

prometheus:
	prometheusSpec:
		retention: %dd
		storageSpec:
			volumeClaimTemplate:
				spec:
					storageClassName: %s
					accessModes: ["ReadWriteOnce"]
					resources:
						requests:
							storage: 40Gi
`, adminUser, adminPassword, storageClass, retentionDays, storageClass)
}
