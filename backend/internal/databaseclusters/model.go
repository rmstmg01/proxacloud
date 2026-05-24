package databaseclusters

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/secrets"
	"k8s-proxmox-manager/backend/internal/sshkeys"
	"k8s-proxmox-manager/backend/internal/terraform"
	"k8s-proxmox-manager/backend/internal/topology"
)

const (
	engineMariaDBGalera = "mariadb-galera"
	nodeCount           = 3
)

var (
	ErrDatabaseClusterExists   = errors.New("database cluster already exists")
	ErrDatabaseClusterNotFound = errors.New("database cluster not found")
)

type NetworkStore interface {
	GetByID(id int64) (networks.Profile, error)
}

type NodeInventory interface {
	ListEnabledNodes() ([]proxmox.Node, error)
}

type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
}

type SSHKeyPairStore interface {
	GetByID(id int64) (sshkeys.KeyPair, error)
}

type PlanRequest struct {
	Name                string   `json:"name"`
	NetworkProfileID    int64    `json:"network_profile_id"`
	ProxmoxCredentialID int64    `json:"proxmox_credential_id"`
	ProxmoxNodeNames    []string `json:"proxmox_node_names"`
	NodeIPPoolStart     string   `json:"node_ip_pool_start"`
	CPU                 int      `json:"cpu"`
	MemoryMB            int      `json:"memory_mb"`
	DiskGB              int      `json:"disk_gb"`
}

type ProvisionRequest struct {
	PlanRequest
	MariaDBVersion     string `json:"mariadb_version"`
	UbuntuImage        string `json:"ubuntu_image"`
	SSHPublicKey       string `json:"ssh_public_key"`
	SSHKeyPairID       int64  `json:"ssh_key_pair_id"`
	DatabaseName       string `json:"database_name"`
	AppUsername        string `json:"app_username"`
	AppPassword        string `json:"app_password"`
	RootPassword       string `json:"root_password"`
	SSTUsername        string `json:"sst_username"`
	SSTPassword        string `json:"sst_password"`
	HAProxyNamespace   string `json:"haproxy_namespace"`
	HAProxyServiceName string `json:"haproxy_service_name"`
	Apply              bool   `json:"apply"`
}

type BootstrapRequest struct {
	SSHUser           string `json:"ssh_user"`
	SSHPrivateKey     string `json:"ssh_private_key"`
	SSHPrivateKeyPath string `json:"ssh_private_key_path"`
	SSHKeyPairID      int64  `json:"ssh_key_pair_id"`
	Apply             bool   `json:"apply"`
}

type NodeRecord struct {
	ID                  int64     `json:"id"`
	NodeIndex           int       `json:"node_index"`
	VMName              string    `json:"vm_name"`
	IPAddress           string    `json:"ip_address"`
	ProxmoxNodeName     string    `json:"proxmox_node_name"`
	ProxmoxCredentialID int64     `json:"proxmox_credential_id"`
	CPU                 int       `json:"cpu"`
	MemoryMB            int       `json:"memory_mb"`
	DiskGB              int       `json:"disk_gb"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ClusterRecord struct {
	ID                  int64         `json:"id"`
	Name                string        `json:"name"`
	Engine              string        `json:"engine"`
	MariaDBVersion      string        `json:"mariadb_version"`
	TopologyType        string        `json:"topology_type"`
	NetworkProfileID    int64         `json:"network_profile_id"`
	ProxmoxCredentialID int64         `json:"proxmox_credential_id"`
	SSHKeyPairID        sql.NullInt64 `json:"-"`
	DatabaseName        string        `json:"database_name"`
	AppUsername         string        `json:"app_username"`
	SSTUsername         string        `json:"sst_username"`
	HAProxyNamespace    string        `json:"haproxy_namespace"`
	HAProxyServiceName  string        `json:"haproxy_service_name"`
	TerraformWorkDir    string        `json:"terraform_work_dir"`
	AnsibleWorkDir      string        `json:"ansible_work_dir"`
	Status              string        `json:"status"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	Nodes               []NodeRecord  `json:"nodes,omitempty"`
}

type RunRecord struct {
	ID         int64      `json:"id"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	WorkDir    string     `json:"work_dir"`
	Output     string     `json:"output"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type PlanResponse struct {
	TopologyType         string       `json:"topology_type"`
	SelectedProxmoxNodes []string     `json:"selected_proxmox_nodes"`
	Nodes                []NodeRecord `json:"nodes"`
	Warnings             []string     `json:"warnings"`
}

type ProvisionResponse struct {
	ClusterID        int64        `json:"cluster_id"`
	Status           string       `json:"status"`
	TerraformWorkDir string       `json:"terraform_work_dir"`
	TerraformOutput  string       `json:"terraform_output,omitempty"`
	Nodes            []NodeRecord `json:"nodes"`
	KubernetesYAML   string       `json:"kubernetes_yaml"`
	Warnings         []string     `json:"warnings"`
}

type BootstrapResponse struct {
	ClusterID      int64    `json:"cluster_id"`
	Status         string   `json:"status"`
	AnsibleWorkDir string   `json:"ansible_work_dir"`
	AnsibleOutput  string   `json:"ansible_output,omitempty"`
	Warnings       []string `json:"warnings"`
}

type KubernetesManifestResponse struct {
	ClusterID int64  `json:"cluster_id"`
	Manifest  string `json:"manifest"`
}

type Service struct {
	db                 *sql.DB
	networks           NetworkStore
	inventory          NodeInventory
	terraformRunner    *terraform.Runner
	proxmoxCredentials ProxmoxCredentialStore
	sshKeys            SSHKeyPairStore
	secrets            secrets.Store
	ansibleBinary      string
	ansibleWorkDir     string
	terraformAutoApply bool
	ansibleAutoApply   bool
}

func NewService(db *sql.DB, networkStore NetworkStore, inventory NodeInventory, terraformRunner *terraform.Runner, proxmoxCredentials ProxmoxCredentialStore, sshKeys SSHKeyPairStore, secretStore secrets.Store, ansibleBinary, ansibleWorkDir string, terraformAutoApply, ansibleAutoApply bool) *Service {
	if strings.TrimSpace(ansibleBinary) == "" {
		ansibleBinary = "ansible-playbook"
	}
	if strings.TrimSpace(ansibleWorkDir) == "" {
		ansibleWorkDir = filepath.Join("infra", "ansible", "runs")
	}
	return &Service{
		db:                 db,
		networks:           networkStore,
		inventory:          inventory,
		terraformRunner:    terraformRunner,
		proxmoxCredentials: proxmoxCredentials,
		sshKeys:            sshKeys,
		secrets:            secretStore,
		ansibleBinary:      ansibleBinary,
		ansibleWorkDir:     ansibleWorkDir,
		terraformAutoApply: terraformAutoApply,
		ansibleAutoApply:   ansibleAutoApply,
	}
}

func (s *Service) Plan(req PlanRequest) (PlanResponse, error) {
	_, nodes, warnings, err := s.buildPlan(req)
	if err != nil {
		return PlanResponse{}, err
	}
	selected := make([]string, 0, len(nodes))
	for _, n := range nodes {
		selected = append(selected, n.ProxmoxNodeName)
	}
	return PlanResponse{
		TopologyType:         topology.TypeTri,
		SelectedProxmoxNodes: selected,
		Nodes:                nodes,
		Warnings:             warnings,
	}, nil
}

func (s *Service) Provision(req ProvisionRequest) (ProvisionResponse, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return ProvisionResponse{}, errors.New("name is required")
	}
	exists, err := s.nameExists(req.Name)
	if err != nil {
		return ProvisionResponse{}, err
	}
	if exists {
		return ProvisionResponse{}, ErrDatabaseClusterExists
	}

	profile, nodes, warnings, err := s.buildPlan(req.PlanRequest)
	if err != nil {
		return ProvisionResponse{}, err
	}
	if req.ProxmoxCredentialID <= 0 && len(nodes) > 0 {
		req.ProxmoxCredentialID = nodes[0].ProxmoxCredentialID
	}
	if hasMultipleNodeCredentials(nodes) {
		warnings = append(warnings, "selected nodes use multiple Proxmox credential records; Terraform apply will authenticate with the first selected node credential")
	}
	if err := s.resolveProvisionSSHKey(&req); err != nil {
		return ProvisionResponse{}, err
	}
	if strings.TrimSpace(req.UbuntuImage) == "" {
		return ProvisionResponse{}, errors.New("ubuntu_image is required")
	}

	req.MariaDBVersion = nonEmpty(req.MariaDBVersion, "11.4")
	req.DatabaseName = nonEmpty(req.DatabaseName, "pscs")
	req.AppUsername = nonEmpty(req.AppUsername, "pscs")
	req.SSTUsername = nonEmpty(req.SSTUsername, "sstuser")
	req.HAProxyNamespace = nonEmpty(req.HAProxyNamespace, "pscs")
	req.HAProxyServiceName = nonEmpty(req.HAProxyServiceName, "external-mariadb")
	if strings.TrimSpace(req.RootPassword) == "" || strings.TrimSpace(req.AppPassword) == "" || strings.TrimSpace(req.SSTPassword) == "" {
		return ProvisionResponse{}, errors.New("root_password, app_password, and sst_password are required")
	}
	if !isSQLIdentifier(req.DatabaseName) || !isSQLIdentifier(req.AppUsername) || !isSQLIdentifier(req.SSTUsername) {
		return ProvisionResponse{}, errors.New("database_name, app_username, and sst_username must contain only letters, numbers, and underscores")
	}
	if containsUnsafeSQLPasswordChars(req.RootPassword, req.AppPassword, req.SSTPassword) {
		return ProvisionResponse{}, errors.New("database passwords cannot contain single quotes or newlines")
	}

	rootRef, appRef, sstRef, err := s.storeDatabaseSecrets(req.Name, req.RootPassword, req.AppPassword, req.SSTPassword)
	if err != nil {
		return ProvisionResponse{}, err
	}

	tfNodes := make([]terraform.VMNode, 0, len(nodes))
	for _, n := range nodes {
		tfNodes = append(tfNodes, terraform.VMNode{
			VMName:          n.VMName,
			Role:            "database",
			RoleIndex:       n.NodeIndex,
			ProxmoxNodeName: n.ProxmoxNodeName,
			CPU:             n.CPU,
			MemoryMB:        n.MemoryMB,
			DiskGB:          n.DiskGB,
			IPAddress:       n.IPAddress,
			NetworkCIDR:     profile.CIDR,
			Gateway:         profile.Gateway,
			Bridge:          profile.Bridge,
			VLAN:            profile.VLAN,
		})
	}

	workDir, err := s.terraformRunner.PrepareWorkspace(terraform.WorkspaceSpec{
		WorkspaceKey:      "db-" + req.Name,
		ClusterName:       req.Name,
		KubernetesVersion: "external-db",
		NetworkCIDR:       profile.CIDR,
		Gateway:           profile.Gateway,
		DNS:               profile.DNS,
		Bridge:            profile.Bridge,
		VLAN:              profile.VLAN,
		UbuntuImage:       req.UbuntuImage,
		SSHPublicKey:      req.SSHPublicKey,
		Nodes:             tfNodes,
	})
	if err != nil {
		return ProvisionResponse{}, err
	}

	clusterID, err := s.insertCluster(req, rootRef, appRef, sstRef, workDir, nodes)
	if err != nil {
		return ProvisionResponse{}, err
	}
	_ = s.insertRun(clusterID, "terraform-prepare", "prepared", workDir, "")

	status := "planned"
	output := ""
	if req.Apply {
		if !s.terraformAutoApply {
			warnings = append(warnings, "terraform apply requested but TERRAFORM_AUTO_APPLY is false; workspace was generated only")
		} else {
			cred, credErr := s.proxmoxCredentials.GetCredentialWithSecrets(req.ProxmoxCredentialID)
			if credErr != nil {
				return ProvisionResponse{}, credErr
			}
			result, applyErr := s.terraformRunner.Apply(workDir, terraform.ProxmoxAuth{
				Endpoint:       cred.Endpoint,
				Username:       cred.Username,
				Password:       cred.Password,
				APITokenID:     cred.APITokenID,
				APITokenSecret: cred.APITokenSecret,
				InsecureTLS:    cred.InsecureTLS,
			})
			output = result.Output
			status = result.Status
			finished := applyErr == nil
			_ = s.insertRun(clusterID, "terraform-apply", result.Status, workDir, result.Output, finished)
			if applyErr != nil {
				_ = s.updateStatus(clusterID, "provision_failed")
				return ProvisionResponse{}, applyErr
			}
			_ = s.updateStatus(clusterID, "provisioned")
		}
	}

	return ProvisionResponse{
		ClusterID:        clusterID,
		Status:           status,
		TerraformWorkDir: workDir,
		TerraformOutput:  output,
		Nodes:            nodes,
		KubernetesYAML:   buildKubernetesManifest(req.HAProxyNamespace, req.HAProxyServiceName, nodes, req.DatabaseName, req.AppUsername),
		Warnings:         warnings,
	}, nil
}

func (s *Service) Bootstrap(clusterID int64, req BootstrapRequest) (BootstrapResponse, error) {
	cluster, err := s.Get(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}
	privateKey, sshUser, err := s.resolveBootstrapSSH(req)
	if err != nil {
		return BootstrapResponse{}, err
	}
	secrets, err := s.loadDatabaseSecrets(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}
	workDir, err := s.prepareAnsibleWorkspace(cluster, privateKey, sshUser, secrets)
	if err != nil {
		return BootstrapResponse{}, err
	}
	_ = s.setAnsibleWorkDir(clusterID, workDir)
	_ = s.insertRun(clusterID, "ansible-prepare", "prepared", workDir, "")

	warnings := []string{}
	status := "prepared"
	output := ""
	if req.Apply {
		if !s.ansibleAutoApply {
			warnings = append(warnings, "ansible apply requested but ANSIBLE_AUTO_APPLY is false; workspace was generated only")
		} else {
			result, applyErr := s.runAnsible(workDir)
			output = result.Output
			status = result.Status
			_ = s.insertRun(clusterID, "ansible-apply", result.Status, workDir, result.Output, applyErr == nil)
			if applyErr != nil {
				_ = s.updateStatus(clusterID, "bootstrap_failed")
				return BootstrapResponse{}, applyErr
			}
			_ = s.updateStatus(clusterID, "ready")
		}
	}

	return BootstrapResponse{ClusterID: clusterID, Status: status, AnsibleWorkDir: workDir, AnsibleOutput: output, Warnings: warnings}, nil
}

func (s *Service) List() ([]ClusterRecord, error) {
	rows, err := s.db.Query(`SELECT id, name, engine, mariadb_version, topology_type, network_profile_id, proxmox_credential_id, ssh_key_pair_id, database_name, app_username, sst_username, haproxy_namespace, haproxy_service_name, terraform_work_dir, ansible_work_dir, status, created_at, updated_at FROM database_clusters ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list database clusters: %w", err)
	}
	defer rows.Close()
	out := []ClusterRecord{}
	for rows.Next() {
		var rec ClusterRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Engine, &rec.MariaDBVersion, &rec.TopologyType, &rec.NetworkProfileID, &rec.ProxmoxCredentialID, &rec.SSHKeyPairID, &rec.DatabaseName, &rec.AppUsername, &rec.SSTUsername, &rec.HAProxyNamespace, &rec.HAProxyServiceName, &rec.TerraformWorkDir, &rec.AnsibleWorkDir, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *Service) Get(clusterID int64) (ClusterRecord, error) {
	var rec ClusterRecord
	err := s.db.QueryRow(`SELECT id, name, engine, mariadb_version, topology_type, network_profile_id, proxmox_credential_id, ssh_key_pair_id, database_name, app_username, sst_username, haproxy_namespace, haproxy_service_name, terraform_work_dir, ansible_work_dir, status, created_at, updated_at FROM database_clusters WHERE id = $1`, clusterID).
		Scan(&rec.ID, &rec.Name, &rec.Engine, &rec.MariaDBVersion, &rec.TopologyType, &rec.NetworkProfileID, &rec.ProxmoxCredentialID, &rec.SSHKeyPairID, &rec.DatabaseName, &rec.AppUsername, &rec.SSTUsername, &rec.HAProxyNamespace, &rec.HAProxyServiceName, &rec.TerraformWorkDir, &rec.AnsibleWorkDir, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterRecord{}, ErrDatabaseClusterNotFound
	}
	if err != nil {
		return ClusterRecord{}, fmt.Errorf("get database cluster: %w", err)
	}
	nodes, err := s.listNodes(clusterID)
	if err != nil {
		return ClusterRecord{}, err
	}
	rec.Nodes = nodes
	return rec, nil
}

func (s *Service) ListRuns(clusterID int64) ([]RunRecord, error) {
	rows, err := s.db.Query(`SELECT id, action, status, work_dir, output, created_at, finished_at FROM database_cluster_runs WHERE database_cluster_id = $1 ORDER BY created_at DESC`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("list database cluster runs: %w", err)
	}
	defer rows.Close()
	out := []RunRecord{}
	for rows.Next() {
		var rec RunRecord
		if err := rows.Scan(&rec.ID, &rec.Action, &rec.Status, &rec.WorkDir, &rec.Output, &rec.CreatedAt, &rec.FinishedAt); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *Service) KubernetesManifest(clusterID int64) (KubernetesManifestResponse, error) {
	cluster, err := s.Get(clusterID)
	if err != nil {
		return KubernetesManifestResponse{}, err
	}
	return KubernetesManifestResponse{ClusterID: clusterID, Manifest: buildKubernetesManifest(cluster.HAProxyNamespace, cluster.HAProxyServiceName, cluster.Nodes, cluster.DatabaseName, cluster.AppUsername)}, nil
}

func (s *Service) buildPlan(req PlanRequest) (networks.Profile, []NodeRecord, []string, error) {
	profile, err := s.networks.GetByID(req.NetworkProfileID)
	if err != nil {
		return networks.Profile{}, nil, nil, err
	}
	if net.ParseIP(strings.TrimSpace(req.NodeIPPoolStart)).To4() == nil {
		return networks.Profile{}, nil, nil, errors.New("node_ip_pool_start must be a valid IPv4 address")
	}

	enabled, err := s.inventory.ListEnabledNodes()
	if err != nil {
		return networks.Profile{}, nil, nil, err
	}
	selected, err := selectProxmoxNodes(enabled, req.ProxmoxCredentialID, req.ProxmoxNodeNames)
	if err != nil {
		return networks.Profile{}, nil, nil, err
	}

	cpu := req.CPU
	if cpu <= 0 {
		cpu = 4
	}
	memory := req.MemoryMB
	if memory <= 0 {
		memory = 8192
	}
	disk := req.DiskGB
	if disk <= 0 {
		disk = 100
	}
	if memory < 4096 {
		return networks.Profile{}, nil, nil, errors.New("memory_mb must be at least 4096 for production Galera nodes")
	}
	if disk < 40 {
		return networks.Profile{}, nil, nil, errors.New("disk_gb must be at least 40 for production Galera nodes")
	}

	nodes := make([]NodeRecord, 0, nodeCount)
	for i, proxmoxNode := range selected {
		ip, err := addIPv4(req.NodeIPPoolStart, i)
		if err != nil {
			return networks.Profile{}, nil, nil, err
		}
		nodes = append(nodes, NodeRecord{
			NodeIndex:           i + 1,
			VMName:              fmt.Sprintf("%s-db-%d", sanitizeName(req.Name), i+1),
			IPAddress:           ip,
			ProxmoxNodeName:     proxmoxNode.NodeName,
			ProxmoxCredentialID: proxmoxNode.CredentialID,
			CPU:                 cpu,
			MemoryMB:            memory,
			DiskGB:              disk,
			Status:              "planned",
		})
	}
	warnings := []string{
		"open TCP ports 3306, 4444, 4567, and 4568 between database VMs before bootstrap",
		"prefer a single writer or application-level retry handling for Galera deadlocks in production",
	}
	return profile, nodes, warnings, nil
}

func selectProxmoxNodes(enabled []proxmox.Node, credentialID int64, requested []string) ([]proxmox.Node, error) {
	if len(requested) > 0 {
		if len(requested) != nodeCount {
			return nil, errors.New("proxmox_node_names must contain exactly three distinct nodes")
		}
		byName := map[string]proxmox.Node{}
		for _, n := range enabled {
			byName[strings.ToLower(n.NodeName)] = n
		}
		out := []proxmox.Node{}
		seen := map[string]struct{}{}
		for _, raw := range requested {
			key := strings.ToLower(strings.TrimSpace(raw))
			if key == "" {
				return nil, errors.New("proxmox_node_names cannot contain empty values")
			}
			if _, exists := seen[key]; exists {
				return nil, errors.New("proxmox_node_names must be distinct")
			}
			node, ok := byName[key]
			if !ok {
				return nil, fmt.Errorf("proxmox node %q is not enabled", raw)
			}
			seen[key] = struct{}{}
			out = append(out, node)
		}
		return out, nil
	}

	candidates := []proxmox.Node{}
	for _, n := range enabled {
		if credentialID <= 0 || n.CredentialID == credentialID {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) < nodeCount {
		return nil, fmt.Errorf("three enabled proxmox nodes are required; found %d", len(candidates))
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i].MemoryTotalBytes - candidates[i].MemoryUsedBytes
		right := candidates[j].MemoryTotalBytes - candidates[j].MemoryUsedBytes
		if left == right {
			return candidates[i].NodeName < candidates[j].NodeName
		}
		return left > right
	})
	return candidates[:nodeCount], nil
}

func hasMultipleNodeCredentials(nodes []NodeRecord) bool {
	seen := map[int64]struct{}{}
	for _, node := range nodes {
		seen[node.ProxmoxCredentialID] = struct{}{}
	}
	return len(seen) > 1
}

func (s *Service) resolveProvisionSSHKey(req *ProvisionRequest) error {
	req.SSHPublicKey = strings.TrimSpace(req.SSHPublicKey)
	if req.SSHPublicKey != "" {
		return nil
	}
	if req.SSHKeyPairID <= 0 {
		return errors.New("ssh_public_key or ssh_key_pair_id is required")
	}
	pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
	if err != nil {
		return err
	}
	req.SSHPublicKey = strings.TrimSpace(pair.PublicKey)
	if req.SSHPublicKey == "" {
		return errors.New("selected ssh key pair has empty public key")
	}
	return nil
}

func (s *Service) resolveBootstrapSSH(req BootstrapRequest) (string, string, error) {
	sshUser := strings.TrimSpace(req.SSHUser)
	if sshUser == "" {
		sshUser = "ubuntu"
	}
	if strings.TrimSpace(req.SSHPrivateKey) != "" {
		return strings.TrimSpace(req.SSHPrivateKey), sshUser, nil
	}
	if req.SSHKeyPairID > 0 {
		pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(pair.SSHUser) != "" {
			sshUser = strings.TrimSpace(pair.SSHUser)
		}
		if strings.TrimSpace(pair.PrivateKey) == "" {
			return "", "", errors.New("selected ssh key pair has empty private key")
		}
		return strings.TrimSpace(pair.PrivateKey), sshUser, nil
	}
	if strings.TrimSpace(req.SSHPrivateKeyPath) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(req.SSHPrivateKeyPath))
		if err != nil {
			return "", "", fmt.Errorf("read ssh private key path: %w", err)
		}
		return strings.TrimSpace(string(raw)), sshUser, nil
	}
	return "", "", errors.New("provide ssh_private_key, ssh_private_key_path, or ssh_key_pair_id")
}

func (s *Service) storeDatabaseSecrets(name, root, app, sst string) (string, string, string, error) {
	scope := "database-clusters/" + sanitizeName(name)
	rootRef, err := s.secrets.Put(context.Background(), scope, "root-password", []byte(strings.TrimSpace(root)))
	if err != nil {
		return "", "", "", fmt.Errorf("store root password: %w", err)
	}
	appRef, err := s.secrets.Put(context.Background(), scope, "app-password", []byte(strings.TrimSpace(app)))
	if err != nil {
		return "", "", "", fmt.Errorf("store app password: %w", err)
	}
	sstRef, err := s.secrets.Put(context.Background(), scope, "sst-password", []byte(strings.TrimSpace(sst)))
	if err != nil {
		return "", "", "", fmt.Errorf("store sst password: %w", err)
	}
	return rootRef, appRef, sstRef, nil
}

type databaseSecrets struct {
	RootPassword string
	AppPassword  string
	SSTPassword  string
}

func (s *Service) loadDatabaseSecrets(clusterID int64) (databaseSecrets, error) {
	var rootRef, appRef, sstRef string
	err := s.db.QueryRow(`SELECT root_password_secret_ref, app_password_secret_ref, sst_password_secret_ref FROM database_clusters WHERE id = $1`, clusterID).Scan(&rootRef, &appRef, &sstRef)
	if err != nil {
		return databaseSecrets{}, err
	}
	root, err := s.secrets.Get(context.Background(), rootRef)
	if err != nil {
		return databaseSecrets{}, fmt.Errorf("load root password: %w", err)
	}
	app, err := s.secrets.Get(context.Background(), appRef)
	if err != nil {
		return databaseSecrets{}, fmt.Errorf("load app password: %w", err)
	}
	sst, err := s.secrets.Get(context.Background(), sstRef)
	if err != nil {
		return databaseSecrets{}, fmt.Errorf("load sst password: %w", err)
	}
	return databaseSecrets{RootPassword: string(root), AppPassword: string(app), SSTPassword: string(sst)}, nil
}

func (s *Service) insertCluster(req ProvisionRequest, rootRef, appRef, sstRef, workDir string, nodes []NodeRecord) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var clusterID int64
	err = tx.QueryRow(`INSERT INTO database_clusters (name, engine, mariadb_version, topology_type, network_profile_id, proxmox_credential_id, ssh_key_pair_id, database_name, app_username, root_password_secret_ref, app_password_secret_ref, sst_username, sst_password_secret_ref, haproxy_namespace, haproxy_service_name, terraform_work_dir, status)
		VALUES ($1, $2, $3, 'tri', $4, $5, NULLIF($6, 0), $7, $8, $9, $10, $11, $12, $13, $14, $15, 'planned')
		RETURNING id`,
		req.Name, engineMariaDBGalera, req.MariaDBVersion, req.NetworkProfileID, req.ProxmoxCredentialID, req.SSHKeyPairID, req.DatabaseName, req.AppUsername, rootRef, appRef, req.SSTUsername, sstRef, req.HAProxyNamespace, req.HAProxyServiceName, workDir).Scan(&clusterID)
	if err != nil {
		return 0, fmt.Errorf("insert database cluster: %w", err)
	}
	for _, n := range nodes {
		_, err = tx.Exec(`INSERT INTO database_cluster_nodes (database_cluster_id, node_index, vm_name, ip_address, proxmox_node_name, proxmox_credential_id, cpu, memory_mb, disk_gb, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'planned')`, clusterID, n.NodeIndex, n.VMName, n.IPAddress, n.ProxmoxNodeName, n.ProxmoxCredentialID, n.CPU, n.MemoryMB, n.DiskGB)
		if err != nil {
			return 0, fmt.Errorf("insert database cluster node: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return clusterID, nil
}

func (s *Service) listNodes(clusterID int64) ([]NodeRecord, error) {
	rows, err := s.db.Query(`SELECT id, node_index, vm_name, ip_address, proxmox_node_name, proxmox_credential_id, cpu, memory_mb, disk_gb, status, created_at, updated_at FROM database_cluster_nodes WHERE database_cluster_id = $1 ORDER BY node_index ASC`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("list database nodes: %w", err)
	}
	defer rows.Close()
	nodes := []NodeRecord{}
	for rows.Next() {
		var n NodeRecord
		if err := rows.Scan(&n.ID, &n.NodeIndex, &n.VMName, &n.IPAddress, &n.ProxmoxNodeName, &n.ProxmoxCredentialID, &n.CPU, &n.MemoryMB, &n.DiskGB, &n.Status, &n.CreatedAt, &n.UpdatedAt); err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

func (s *Service) nameExists(name string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM database_clusters WHERE LOWER(name) = LOWER($1))`, strings.TrimSpace(name)).Scan(&exists)
	return exists, err
}

func (s *Service) insertRun(clusterID int64, action, status, workDir, output string, finished ...bool) error {
	var finishedAt any
	if len(finished) > 0 && finished[0] {
		finishedAt = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO database_cluster_runs (database_cluster_id, action, status, work_dir, output, finished_at) VALUES ($1, $2, $3, $4, $5, $6)`, clusterID, action, status, workDir, output, finishedAt)
	return err
}

func (s *Service) updateStatus(clusterID int64, status string) error {
	_, err := s.db.Exec(`UPDATE database_clusters SET status = $2, updated_at = NOW() WHERE id = $1`, clusterID, status)
	return err
}

func (s *Service) setAnsibleWorkDir(clusterID int64, workDir string) error {
	_, err := s.db.Exec(`UPDATE database_clusters SET ansible_work_dir = $2, updated_at = NOW() WHERE id = $1`, clusterID, workDir)
	return err
}

type ansibleResult struct {
	WorkDir string
	Output  string
	Status  string
}

func (s *Service) prepareAnsibleWorkspace(cluster ClusterRecord, privateKey, sshUser string, secrets databaseSecrets) (string, error) {
	workDir := filepath.Join(s.ansibleWorkDir, "db-"+sanitizeName(cluster.Name))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("create ansible workdir: %w", err)
	}
	keyPath := filepath.Join(workDir, "id_bootstrap")
	if err := os.WriteFile(keyPath, []byte(strings.TrimSpace(privateKey)+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write bootstrap ssh key: %w", err)
	}
	inventory, err := renderDatabaseInventory(cluster.Nodes, sshUser, keyPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "inventory.ini"), []byte(inventory), 0o644); err != nil {
		return "", fmt.Errorf("write inventory: %w", err)
	}
	playbook, err := renderDatabasePlaybook(cluster, secrets)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "bootstrap-mariadb-galera.yml"), []byte(playbook), 0o600); err != nil {
		return "", fmt.Errorf("write playbook: %w", err)
	}
	readme := buildAnsibleReadme(cluster)
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644); err != nil {
		return "", fmt.Errorf("write ansible readme: %w", err)
	}
	return workDir, nil
}

func (s *Service) runAnsible(workDir string) (ansibleResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ansibleBinary, "-i", "inventory.ini", "bootstrap-mariadb-galera.yml")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	combined := stdout.String() + stderr.String()
	if err != nil {
		return ansibleResult{WorkDir: workDir, Output: combined, Status: "failed"}, fmt.Errorf("run ansible playbook: %w\nansible output:\n%s", err, strings.TrimSpace(combined))
	}
	return ansibleResult{WorkDir: workDir, Output: combined, Status: "bootstrapped"}, nil
}

func renderDatabaseInventory(nodes []NodeRecord, sshUser, keyPath string) (string, error) {
	tpl, err := template.New("inventory").Parse(databaseInventoryTemplate)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = tpl.Execute(&out, map[string]any{"Nodes": nodes, "SSHUser": sshUser, "KeyPath": keyPath})
	return out.String(), err
}

func renderDatabasePlaybook(cluster ClusterRecord, secret databaseSecrets) (string, error) {
	tpl, err := template.New("playbook").Parse(databasePlaybookTemplate)
	if err != nil {
		return "", err
	}
	addresses := []string{}
	for _, n := range cluster.Nodes {
		addresses = append(addresses, n.IPAddress)
	}
	payload := map[string]any{
		"ClusterName":    sanitizeName(cluster.Name),
		"ClusterAddress": "gcomm://" + strings.Join(addresses, ","),
		"DatabaseName":   cluster.DatabaseName,
		"AppUsername":    cluster.AppUsername,
		"AppPassword":    secret.AppPassword,
		"RootPassword":   secret.RootPassword,
		"SSTUsername":    cluster.SSTUsername,
		"SSTPassword":    secret.SSTPassword,
		"PrimaryNode":    cluster.Nodes[0].VMName,
	}
	var out bytes.Buffer
	err = tpl.Execute(&out, payload)
	return out.String(), err
}

func buildKubernetesManifest(namespace, serviceName string, nodes []NodeRecord, databaseName, appUser string) string {
	addresses := strings.Builder{}
	for _, n := range nodes {
		addresses.WriteString(fmt.Sprintf("  - ip: %s\n", n.IPAddress))
	}
	return fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  type: ClusterIP
  ports:
    - name: mysql
      port: 3306
      targetPort: 3306
---
apiVersion: v1
kind: Endpoints
metadata:
  name: %s
  namespace: %s
subsets:
  - addresses:
%s    ports:
      - name: mysql
        port: 3306
---
apiVersion: v1
kind: Secret
metadata:
  name: external-mariadb-client
  namespace: %s
type: Opaque
stringData:
  DB_HOST: %s
  DB_PORT: "3306"
  DB_NAME: %s
  DB_USER: %s
  DB_PASS: replace-me-with-the-app-password-from-your-secret-store
`, serviceName, namespace, serviceName, namespace, addresses.String(), namespace, serviceName, databaseName, appUser)
}

func buildAnsibleReadme(cluster ClusterRecord) string {
	return fmt.Sprintf("# MariaDB Galera Bootstrap\n\nCluster: %s\n\nRun manually:\n\n```bash\nANSIBLE_HOST_KEY_CHECKING=False ansible-playbook -i inventory.ini bootstrap-mariadb-galera.yml\n```\n\nAfter bootstrap, verify:\n\n```bash\nmariadb -uroot -p -e \"SHOW STATUS LIKE 'wsrep_cluster_size'; SHOW STATUS LIKE 'wsrep_local_state_comment'; SHOW STATUS LIKE 'wsrep_ready';\"\n```\n", cluster.Name)
}

func addIPv4(start string, offset int) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(start)).To4()
	if ip == nil {
		return "", errors.New("invalid IPv4 address")
	}
	value := int(ip[0])<<24 | int(ip[1])<<16 | int(ip[2])<<8 | int(ip[3])
	value += offset
	return fmt.Sprintf("%d.%d.%d.%d", byte(value>>24), byte(value>>16), byte(value>>8), byte(value)), nil
}

func sanitizeName(name string) string {
	token := strings.ToLower(strings.TrimSpace(name))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "_", "-")
	token = replacer.Replace(token)
	if token == "" {
		return "database"
	}
	return token
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func isSQLIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func containsUnsafeSQLPasswordChars(values ...string) bool {
	for _, value := range values {
		if strings.ContainsAny(value, "'\r\n") {
			return true
		}
	}
	return false
}

const databaseInventoryTemplate = `[mariadb_galera]
{{- range .Nodes }}
{{ .VMName }} ansible_host={{ .IPAddress }} galera_node_index={{ .NodeIndex }}
{{- end }}

[all:vars]
ansible_user={{ .SSHUser }}
ansible_ssh_private_key_file={{ .KeyPath }}
ansible_python_interpreter=/usr/bin/python3
ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o GlobalKnownHostsFile=/dev/null -o ConnectTimeout=10'
`

const databasePlaybookTemplate = `---
- hosts: mariadb_galera
  become: true
  gather_facts: true
  vars:
    galera_cluster_name: {{ .ClusterName | printf "%q" }}
    galera_cluster_address: {{ .ClusterAddress | printf "%q" }}
    sst_user: {{ .SSTUsername | printf "%q" }}
    sst_password: {{ .SSTPassword | printf "%q" }}
  pre_tasks:
    - name: Wait for cloud-init to finish
      shell: |
        if command -v cloud-init >/dev/null 2>&1; then
          cloud-init status --wait
        fi
      changed_when: false
  tasks:
    - name: Install MariaDB Galera packages
      apt:
        name:
          - mariadb-server
          - galera-4
          - mariadb-backup
          - rsync
          - socat
          - qemu-guest-agent
        update_cache: true
        lock_timeout: 600
        state: present

    - name: Enable qemu guest agent
      service:
        name: qemu-guest-agent
        enabled: true
        state: started

    - name: Write Galera configuration
      copy:
        dest: /etc/mysql/mariadb.conf.d/60-galera.cnf
        owner: root
        group: root
        mode: '0644'
        content: |
          [mysqld]
          bind-address=0.0.0.0
          binlog_format=ROW
          default_storage_engine=InnoDB
          innodb_autoinc_lock_mode=2
          innodb_flush_log_at_trx_commit=1
          innodb_buffer_pool_size=1G

          wsrep_on=ON
          wsrep_provider=/usr/lib/galera/libgalera_smm.so
          wsrep_cluster_name={{ "{{ galera_cluster_name }}" }}
          wsrep_cluster_address={{ "{{ galera_cluster_address }}" }}
          wsrep_node_address={{ "{{ ansible_host }}" }}
          wsrep_node_name={{ "{{ inventory_hostname }}" }}
          wsrep_sst_method=mariabackup
          wsrep_sst_auth={{ "{{ sst_user }}" }}:{{ "{{ sst_password }}" }}

          [sst]
          user={{ "{{ sst_user }}" }}
          password={{ "{{ sst_password }}" }}

    - name: Stop MariaDB before cluster bootstrap
      service:
        name: mariadb
        state: stopped

- hosts: {{ .PrimaryNode }}
  become: true
  tasks:
    - name: Bootstrap first Galera node
      command: galera_new_cluster
      args:
        creates: /root/.pscs-galera-bootstrapped

    - name: Ensure first node is running
      service:
        name: mariadb
        state: started
        enabled: true

    - name: Mark Galera bootstrap complete
      file:
        path: /root/.pscs-galera-bootstrapped
        state: touch
        mode: '0600'

    - name: Set root password and application users
      shell: |
        mariadb -uroot <<'SQL'
        ALTER USER 'root'@'localhost' IDENTIFIED BY '{{ .RootPassword }}';
        CREATE USER IF NOT EXISTS '{{ .SSTUsername }}'@'localhost' IDENTIFIED BY '{{ .SSTPassword }}';
        CREATE USER IF NOT EXISTS '{{ .SSTUsername }}'@'%' IDENTIFIED BY '{{ .SSTPassword }}';
        GRANT RELOAD, PROCESS, LOCK TABLES, REPLICATION CLIENT ON *.* TO '{{ .SSTUsername }}'@'localhost';
        GRANT RELOAD, PROCESS, LOCK TABLES, REPLICATION CLIENT ON *.* TO '{{ .SSTUsername }}'@'%';
        CREATE DATABASE IF NOT EXISTS {{ .DatabaseName }};
        CREATE USER IF NOT EXISTS '{{ .AppUsername }}'@'%' IDENTIFIED BY '{{ .AppPassword }}';
        GRANT ALL PRIVILEGES ON {{ .DatabaseName }}.* TO '{{ .AppUsername }}'@'%';
        FLUSH PRIVILEGES;
        SQL
      args:
        creates: /root/.pscs-galera-users-created

    - name: Mark users as created
      file:
        path: /root/.pscs-galera-users-created
        state: touch
        mode: '0600'

- hosts: mariadb_galera:!{{ .PrimaryNode }}
  become: true
  serial: 1
  tasks:
    - name: Start joining Galera node
      service:
        name: mariadb
        state: started
        enabled: true

    - name: Wait for joiner MySQL port
      wait_for:
        host: "{{ "{{ ansible_host }}" }}"
        port: 3306
        timeout: 120

- hosts: {{ .PrimaryNode }}
  become: true
  tasks:
    - name: Verify Galera cluster size
      shell: |
        mariadb -uroot -p'{{ .RootPassword }}' -NBe "SHOW STATUS LIKE 'wsrep_cluster_size';" | awk '{print $2}'
      register: galera_cluster_size
      retries: 20
      delay: 10
      until: galera_cluster_size.stdout | trim == "3"
      changed_when: false

    - name: Verify Galera is ready
      shell: |
        mariadb -uroot -p'{{ .RootPassword }}' -NBe "SHOW STATUS LIKE 'wsrep_ready';" | awk '{print $2}'
      register: galera_ready
      retries: 10
      delay: 5
      until: galera_ready.stdout | trim == "ON"
      changed_when: false
`
