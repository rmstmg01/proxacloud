package databaseclusters

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
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
	ErrDatabaseClusterExists           = errors.New("database cluster already exists")
	ErrDatabaseClusterNotFound         = errors.New("database cluster not found")
	ErrDatabaseClusterDeleteNotAllowed = errors.New("only failed, provisioned, or ready database clusters can be deleted")
	ErrDatabaseClusterConfirmation     = errors.New("database cluster name confirmation does not match")
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

type EventRecord struct {
	ID        int64     `json:"id"`
	ClusterID int64     `json:"cluster_id"`
	Step      string    `json:"step"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
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

type TerraformApplyResponse struct {
	ClusterID        int64    `json:"cluster_id"`
	Status           string   `json:"status"`
	TerraformWorkDir string   `json:"terraform_work_dir"`
	TerraformOutput  string   `json:"terraform_output,omitempty"`
	Warnings         []string `json:"warnings"`
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

	nodeAddresses, err := s.proxmoxNodeAddresses(nodes)
	if err != nil {
		return ProvisionResponse{}, err
	}

	tfNodes := make([]terraform.VMNode, 0, len(nodes))
	for _, n := range nodes {
		tfNodes = append(tfNodes, terraform.VMNode{
			VMName:             n.VMName,
			Role:               "database",
			RoleIndex:          n.NodeIndex,
			ProxmoxNodeName:    n.ProxmoxNodeName,
			ProxmoxNodeAddress: nodeAddresses[n.ProxmoxNodeName],
			CPU:                n.CPU,
			MemoryMB:           n.MemoryMB,
			DiskGB:             n.DiskGB,
			IPAddress:          n.IPAddress,
			NetworkCIDR:        profile.CIDR,
			Gateway:            profile.Gateway,
			Bridge:             profile.Bridge,
			VLAN:               profile.VLAN,
		})
	}

	vmGroups, err := groupTerraformNodesByCredential(tfNodes, nodes)
	if err != nil {
		return ProvisionResponse{}, err
	}
	credentialIDs := sortedDatabaseCredentialIDs(vmGroups)
	if len(credentialIDs) == 0 {
		return ProvisionResponse{}, errors.New("unable to resolve credential groups for database provisioning")
	}

	workspaces := make(map[int64]string, len(credentialIDs))
	workDir := ""
	for _, credentialID := range credentialIDs {
		groupNodes := vmGroups[credentialID]
		workspaceKey := "db-" + req.Name
		if len(credentialIDs) > 1 {
			workspaceKey = fmt.Sprintf("db-%s-cred-%d", req.Name, credentialID)
		}
		groupWorkDir, prepErr := s.terraformRunner.PrepareWorkspace(terraform.WorkspaceSpec{
			WorkspaceKey:      workspaceKey,
			ClusterName:       req.Name,
			KubernetesVersion: "external-db",
			NetworkCIDR:       profile.CIDR,
			Gateway:           profile.Gateway,
			DNS:               profile.DNS,
			Bridge:            profile.Bridge,
			VLAN:              profile.VLAN,
			UbuntuImage:       req.UbuntuImage,
			SSHPublicKey:      req.SSHPublicKey,
			Nodes:             groupNodes,
		})
		if prepErr != nil {
			return ProvisionResponse{}, prepErr
		}
		workspaces[credentialID] = groupWorkDir
		if workDir == "" {
			workDir = groupWorkDir
		}
	}

	clusterID, err := s.insertCluster(req, rootRef, appRef, sstRef, workDir, nodes)
	if err != nil {
		return ProvisionResponse{}, err
	}
	_ = s.insertRun(clusterID, "terraform-prepare", "prepared", workDir, "")
	_ = s.insertEvent(clusterID, "provision", "info", "Database cluster record created", "")
	_ = s.insertEvent(clusterID, "placement", "info", "Selected Proxmox placement for database VMs", describeNodePlacement(nodes))
	_ = s.insertEvent(clusterID, "terraform-prepare", "success", "Terraform workspace generated", describeWorkspaces(workspaces))

	status := "planned"
	outputs := make([]string, 0, len(credentialIDs))
	if req.Apply {
		_ = s.insertEvent(clusterID, "terraform-apply", "info", "Starting Terraform apply to create database VMs", "")
		for _, credentialID := range credentialIDs {
			groupWorkDir := workspaces[credentialID]
			cred, credErr := s.proxmoxCredentials.GetCredentialWithSecrets(credentialID)
			if credErr != nil {
				_ = s.insertEvent(clusterID, fmt.Sprintf("terraform-apply-cred-%d", credentialID), "error", "Failed to load Proxmox credentials", credErr.Error())
				_ = s.updateStatus(clusterID, "provision_failed")
				return ProvisionResponse{}, credErr
			}
			_ = s.insertEvent(clusterID, fmt.Sprintf("terraform-apply-cred-%d", credentialID), "info", fmt.Sprintf("Applying Terraform for credential_id=%d", credentialID), groupWorkDir)
			result, applyErr := s.terraformRunner.Apply(groupWorkDir, terraform.ProxmoxAuth{
				Endpoint:       cred.Endpoint,
				Username:       cred.Username,
				Password:       cred.Password,
				APITokenID:     cred.APITokenID,
				APITokenSecret: cred.APITokenSecret,
				InsecureTLS:    cred.InsecureTLS,
				SSHUsername:    sshUserFromCredential(cred),
				SSHPassword:    cred.Password,
			})
			outputs = append(outputs, fmt.Sprintf("### credential_id=%d workspace=%s\n%s", credentialID, groupWorkDir, strings.TrimSpace(result.Output)))
			_ = s.insertRun(clusterID, "terraform-apply", result.Status, groupWorkDir, result.Output, applyErr == nil)
			if applyErr != nil {
				combinedOutput := strings.Join(outputs, "\n\n")
				_ = s.updateStatus(clusterID, "provision_failed")
				_ = s.insertEvent(clusterID, fmt.Sprintf("terraform-apply-cred-%d", credentialID), "error", "Terraform apply failed", combinedOutput+"\n"+applyErr.Error())
				return ProvisionResponse{}, applyErr
			}
			_ = s.insertEvent(clusterID, fmt.Sprintf("terraform-apply-cred-%d", credentialID), "success", "Terraform apply completed for credential group", result.Output)
		}
		status = "applied"
		_ = s.updateStatus(clusterID, "provisioned")
		_ = s.insertEvent(clusterID, "terraform-apply", "success", "Database VMs created successfully", strings.Join(outputs, "\n\n"))
		if manifestErr := s.applyKubernetesEndpointManifest(clusterID, req.HAProxyNamespace, req.HAProxyServiceName, nodes); manifestErr != nil {
			warnings = append(warnings, "Kubernetes external MariaDB Service/Endpoints apply failed; see Event Log")
		}
	} else {
		_ = s.insertEvent(clusterID, "terraform-apply", "warning", "Terraform apply skipped by request; no Proxmox VMs were created", "")
	}

	return ProvisionResponse{
		ClusterID:        clusterID,
		Status:           status,
		TerraformWorkDir: workDir,
		TerraformOutput:  strings.Join(outputs, "\n\n"),
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
	if req.SSHKeyPairID <= 0 && cluster.SSHKeyPairID.Valid {
		req.SSHKeyPairID = cluster.SSHKeyPairID.Int64
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
	_ = s.insertEvent(clusterID, "ansible-prepare", "success", "Ansible Galera bootstrap workspace generated", workDir)

	warnings := []string{}
	status := "prepared"
	output := ""
	if req.Apply {
		_ = s.insertEvent(clusterID, "ansible-apply", "info", "Starting Ansible Galera bootstrap", "")
		status = "bootstrapping"
		_ = s.updateStatus(clusterID, status)
		_ = s.insertRun(clusterID, "ansible-apply", status, workDir, "")
		go s.runBootstrapApplyAsync(clusterID, workDir)
	} else {
		_ = s.insertEvent(clusterID, "ansible-apply", "warning", "Ansible apply skipped by request; Galera was not bootstrapped", "")
	}

	return BootstrapResponse{ClusterID: clusterID, Status: status, AnsibleWorkDir: workDir, AnsibleOutput: output, Warnings: warnings}, nil
}

func (s *Service) runBootstrapApplyAsync(clusterID int64, workDir string) {
	result, applyErr := s.runAnsible(workDir)
	_ = s.insertRun(clusterID, "ansible-apply", result.Status, workDir, result.Output, applyErr == nil)
	if applyErr != nil {
		_ = s.updateStatus(clusterID, "bootstrap_failed")
		_ = s.insertEvent(clusterID, "ansible-apply", "error", "Ansible Galera bootstrap failed", result.Output+"\n"+applyErr.Error())
		return
	}
	_ = s.updateStatus(clusterID, "ready")
	_ = s.insertEvent(clusterID, "ansible-apply", "success", "MariaDB Galera bootstrap completed successfully", result.Output)
	if err := s.applyKubernetesAppDatabaseConfigAfterBootstrap(clusterID); err != nil {
		_ = s.insertEvent(clusterID, "kubernetes-app-config", "error", "Failed to connect Kubernetes app to MariaDB Galera cluster", err.Error())
	}
}

func (s *Service) ApplyTerraform(clusterID int64) (TerraformApplyResponse, error) {
	cluster, err := s.Get(clusterID)
	if err != nil {
		return TerraformApplyResponse{}, err
	}
	workDir := strings.TrimSpace(cluster.TerraformWorkDir)
	if workDir == "" {
		return TerraformApplyResponse{}, errors.New("database cluster has no terraform workspace; provision it first")
	}

	warnings := []string{}
	if hasMultipleNodeCredentials(cluster.Nodes) {
		warnings = append(warnings, "selected nodes use multiple Proxmox credential records; Terraform apply authenticates with the first selected node credential")
	}

	credentialID := cluster.ProxmoxCredentialID
	if credentialID <= 0 && len(cluster.Nodes) > 0 {
		credentialID = cluster.Nodes[0].ProxmoxCredentialID
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(credentialID)
	if err != nil {
		_ = s.insertEvent(clusterID, "terraform-apply", "error", "Failed to load Proxmox credentials", err.Error())
		return TerraformApplyResponse{}, err
	}

	_ = s.insertEvent(clusterID, "terraform-apply", "info", "Starting Terraform apply to create database VMs", "")
	result, applyErr := s.terraformRunner.Apply(workDir, terraform.ProxmoxAuth{
		Endpoint:       cred.Endpoint,
		Username:       cred.Username,
		Password:       cred.Password,
		APITokenID:     cred.APITokenID,
		APITokenSecret: cred.APITokenSecret,
		InsecureTLS:    cred.InsecureTLS,
	})
	_ = s.insertRun(clusterID, "terraform-apply", result.Status, workDir, result.Output, applyErr == nil)
	if applyErr != nil {
		_ = s.updateStatus(clusterID, "provision_failed")
		_ = s.insertEvent(clusterID, "terraform-apply", "error", "Terraform apply failed", result.Output+"\n"+applyErr.Error())
		return TerraformApplyResponse{}, applyErr
	}
	_ = s.updateStatus(clusterID, "provisioned")
	_ = s.insertEvent(clusterID, "terraform-apply", "success", "Database VMs created successfully", result.Output)
	if manifestErr := s.applyKubernetesEndpointManifest(clusterID, cluster.HAProxyNamespace, cluster.HAProxyServiceName, cluster.Nodes); manifestErr != nil {
		warnings = append(warnings, "Kubernetes external MariaDB Service/Endpoints apply failed; see Event Log")
	}

	return TerraformApplyResponse{
		ClusterID:        clusterID,
		Status:           result.Status,
		TerraformWorkDir: workDir,
		TerraformOutput:  result.Output,
		Warnings:         warnings,
	}, nil
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

func (s *Service) Delete(clusterID int64, confirmationName string) error {
	cluster, err := s.Get(clusterID)
	if err != nil {
		return err
	}
	if !isDatabaseClusterDeletable(cluster.Status) {
		return ErrDatabaseClusterDeleteNotAllowed
	}
	if strings.TrimSpace(confirmationName) != cluster.Name {
		return ErrDatabaseClusterConfirmation
	}

	if err := s.destroyDatabaseClusterInfrastructure(cluster); err != nil {
		_ = s.insertEvent(clusterID, "terraform-destroy", "error", "Terraform destroy failed during database cluster deletion", err.Error())
		return err
	}

	res, err := s.db.Exec(`DELETE FROM database_clusters WHERE id = $1`, clusterID)
	if err != nil {
		return fmt.Errorf("delete database cluster: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrDatabaseClusterNotFound
	}
	s.removeDatabaseClusterWorkspaces(cluster)
	return nil
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

func (s *Service) ListEvents(clusterID int64) ([]EventRecord, error) {
	rows, err := s.db.Query(`SELECT id, database_cluster_id, step, level, message, output, created_at FROM database_cluster_events WHERE database_cluster_id = $1 ORDER BY id ASC`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("list database cluster events: %w", err)
	}
	defer rows.Close()

	out := []EventRecord{}
	for rows.Next() {
		var rec EventRecord
		if err := rows.Scan(&rec.ID, &rec.ClusterID, &rec.Step, &rec.Level, &rec.Message, &rec.Output, &rec.CreatedAt); err == nil {
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

func describeNodePlacement(nodes []NodeRecord) string {
	var builder strings.Builder
	for _, node := range nodes {
		builder.WriteString(fmt.Sprintf("%s -> %s (%s)\n", node.VMName, node.ProxmoxNodeName, node.IPAddress))
	}
	return strings.TrimSpace(builder.String())
}

func groupTerraformNodesByCredential(tfNodes []terraform.VMNode, records []NodeRecord) (map[int64][]terraform.VMNode, error) {
	credentialByNode := make(map[string]int64, len(records))
	for _, record := range records {
		nodeName := strings.TrimSpace(record.ProxmoxNodeName)
		if nodeName != "" && record.ProxmoxCredentialID > 0 {
			credentialByNode[nodeName] = record.ProxmoxCredentialID
		}
	}

	out := make(map[int64][]terraform.VMNode)
	for _, node := range tfNodes {
		nodeName := strings.TrimSpace(node.ProxmoxNodeName)
		credentialID := credentialByNode[nodeName]
		if credentialID <= 0 {
			return nil, fmt.Errorf("missing proxmox credential mapping for node %q", nodeName)
		}
		out[credentialID] = append(out[credentialID], node)
	}
	return out, nil
}

func sortedDatabaseCredentialIDs(groups map[int64][]terraform.VMNode) []int64 {
	out := make([]int64, 0, len(groups))
	for credentialID := range groups {
		if credentialID > 0 {
			out = append(out, credentialID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func describeWorkspaces(workspaces map[int64]string) string {
	ids := make([]int64, 0, len(workspaces))
	for credentialID := range workspaces {
		ids = append(ids, credentialID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var builder strings.Builder
	for _, credentialID := range ids {
		builder.WriteString(fmt.Sprintf("credential_id=%d -> %s\n", credentialID, workspaces[credentialID]))
	}
	return strings.TrimSpace(builder.String())
}

func isDatabaseClusterDeletable(status string) bool {
	status = strings.TrimSpace(status)
	return status == "provisioned" || status == "ready" || status == "applied" || strings.HasSuffix(status, "_failed")
}

func (s *Service) destroyDatabaseClusterInfrastructure(cluster ClusterRecord) error {
	credentialIDs := distinctDatabaseCredentialIDs(cluster.Nodes)
	if len(credentialIDs) == 0 && cluster.ProxmoxCredentialID > 0 {
		credentialIDs = append(credentialIDs, cluster.ProxmoxCredentialID)
	}
	if len(credentialIDs) == 0 {
		if len(cluster.Nodes) > 0 {
			return errors.New("database cluster cannot be deleted safely because its Proxmox credential metadata is missing; destroy the VMs manually first")
		}
		return nil
	}

	multiCredential := len(credentialIDs) > 1
	for _, credentialID := range credentialIDs {
		cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(credentialID)
		if err != nil {
			return fmt.Errorf("load Proxmox credential for database cluster delete: %w", err)
		}
		auth := terraform.ProxmoxAuth{
			Endpoint:       cred.Endpoint,
			Username:       cred.Username,
			Password:       cred.Password,
			APITokenID:     cred.APITokenID,
			APITokenSecret: cred.APITokenSecret,
			InsecureTLS:    cred.InsecureTLS,
			SSHUsername:    sshUserFromCredential(cred),
			SSHPassword:    cred.Password,
		}

		candidates := databaseWorkspaceCandidates(s.terraformRunner, cluster, credentialID, multiCredential)
		destroyed := false
		seen := make(map[string]struct{}, len(candidates))
		for _, candidate := range candidates {
			workDir := strings.TrimSpace(candidate)
			if workDir == "" {
				continue
			}
			if _, ok := seen[workDir]; ok {
				continue
			}
			seen[workDir] = struct{}{}

			if _, statErr := os.Stat(filepath.Join(workDir, "terraform.tfvars.json")); statErr == nil {
				result, destroyErr := s.terraformRunner.Destroy(workDir, auth)
				_ = s.insertRun(cluster.ID, "terraform-destroy", result.Status, workDir, result.Output, destroyErr == nil)
				if destroyErr != nil {
					if isIgnorableDatabaseDestroyError(destroyErr) {
						log.Printf("warning: terraform destroy failed for database cluster %d credential_id=%d workspace=%s; proceeding with metadata delete only: %v", cluster.ID, credentialID, workDir, destroyErr)
						destroyed = true
						break
					}
					return fmt.Errorf("destroy database cluster infrastructure: %w", destroyErr)
				}
				destroyed = true
				break
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("inspect terraform workspace: %w", statErr)
			}
		}

		if !destroyed && len(cluster.Nodes) > 0 {
			log.Printf("warning: terraform workspace for database cluster %d credential_id=%d is missing; proceeding with metadata delete only", cluster.ID, credentialID)
		}
	}
	return nil
}

func databaseWorkspaceCandidates(runner *terraform.Runner, cluster ClusterRecord, credentialID int64, multiCredential bool) []string {
	if multiCredential {
		return []string{runner.WorkspaceDirForKey(databaseWorkspaceKeyForCredential(cluster.Name, credentialID))}
	}
	return []string{
		cluster.TerraformWorkDir,
		runner.WorkspaceDirForKey("db-" + cluster.Name),
		runner.WorkspaceDir(cluster.Name),
	}
}

func (s *Service) removeDatabaseClusterWorkspaces(cluster ClusterRecord) {
	removeDatabaseWorkspace(cluster.TerraformWorkDir)
	removeDatabaseWorkspace(s.terraformRunner.WorkspaceDirForKey("db-" + cluster.Name))
	removeDatabaseWorkspace(s.terraformRunner.WorkspaceDir(cluster.Name))
	for _, credentialID := range distinctDatabaseCredentialIDs(cluster.Nodes) {
		removeDatabaseWorkspace(s.terraformRunner.WorkspaceDirForKey(databaseWorkspaceKeyForCredential(cluster.Name, credentialID)))
	}
}

func removeDatabaseWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.RemoveAll(path)
}

func databaseWorkspaceKeyForCredential(clusterName string, credentialID int64) string {
	return fmt.Sprintf("db-%s-cred-%d", strings.TrimSpace(clusterName), credentialID)
}

func distinctDatabaseCredentialIDs(nodes []NodeRecord) []int64 {
	seen := make(map[int64]struct{}, len(nodes))
	out := make([]int64, 0)
	for _, node := range nodes {
		if node.ProxmoxCredentialID <= 0 {
			continue
		}
		if _, ok := seen[node.ProxmoxCredentialID]; ok {
			continue
		}
		seen[node.ProxmoxCredentialID] = struct{}{}
		out = append(out, node.ProxmoxCredentialID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func isIgnorableDatabaseDestroyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "hostname lookup") ||
		strings.Contains(msg, "name or service not known") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection refused") ||
		strings.Contains(msg, "server misbehaving") ||
		strings.Contains(msg, "i/o timeout")
}

func sshUserFromCredential(cred proxmox.Credential) string {
	if cred.Username != "" {
		return strings.SplitN(cred.Username, "@", 2)[0]
	}
	if cred.APITokenID != "" {
		return strings.SplitN(cred.APITokenID, "@", 2)[0]
	}
	return ""
}

func (s *Service) proxmoxNodeAddresses(nodes []NodeRecord) (map[string]string, error) {
	out := make(map[string]string, len(nodes))
	credentials := make(map[int64]proxmox.Credential)
	for _, node := range nodes {
		if strings.TrimSpace(node.ProxmoxNodeName) == "" || node.ProxmoxCredentialID <= 0 {
			continue
		}
		cred, ok := credentials[node.ProxmoxCredentialID]
		if !ok {
			var err error
			cred, err = s.proxmoxCredentials.GetCredentialWithSecrets(node.ProxmoxCredentialID)
			if err != nil {
				return nil, fmt.Errorf("load proxmox credential for node %s: %w", node.ProxmoxNodeName, err)
			}
			credentials[node.ProxmoxCredentialID] = cred
		}
		address := endpointHost(cred.Endpoint)
		if address != "" {
			out[node.ProxmoxNodeName] = address
		}
	}
	return out, nil
}

func endpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		host = strings.TrimSpace(parsed.Host)
	}
	if host == "" {
		return ""
	}
	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = withoutPort
	}
	return strings.Trim(host, "[]")
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

func (s *Service) insertEvent(clusterID int64, step, level, message, output string) error {
	if clusterID <= 0 {
		return nil
	}
	step = strings.TrimSpace(step)
	level = strings.TrimSpace(level)
	message = strings.TrimSpace(message)
	if step == "" {
		step = "database-cluster"
	}
	if level == "" {
		level = "info"
	}
	if message == "" {
		message = "event recorded"
	}
	_, err := s.db.Exec(`INSERT INTO database_cluster_events (database_cluster_id, step, level, message, output) VALUES ($1, $2, $3, $4, $5)`, clusterID, step, level, message, output)
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

func (s *Service) applyKubernetesEndpointManifest(clusterID int64, namespace, serviceName string, nodes []NodeRecord) error {
	namespace = nonEmpty(namespace, "pscs")
	serviceName = nonEmpty(serviceName, "external-mariadb")
	manifest := buildKubernetesEndpointManifest(namespace, serviceName, nodes)

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		_ = s.insertEvent(clusterID, "kubernetes-manifest", "error", "Kubernetes kubeconfig is not available for external MariaDB manifest apply", err.Error())
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_ = s.insertEvent(clusterID, "kubernetes-manifest", "info", fmt.Sprintf("Applying external MariaDB Service/Endpoints %s/%s", namespace, serviceName), manifest)

	outputs := make([]string, 0, 2)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "cluster-info")
	output, err := cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		combined := strings.Join(outputs, "\n")
		_ = s.insertEvent(clusterID, "kubernetes-manifest", "error", "kubectl cluster connectivity test failed", combined+"\n"+err.Error())
		return fmt.Errorf("kubectl cluster connectivity test failed: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(manifest)
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	combined := strings.Join(outputs, "\n")
	if err != nil {
		_ = s.insertEvent(clusterID, "kubernetes-manifest", "error", "Failed to apply external MariaDB Service/Endpoints manifest", combined+"\n"+err.Error())
		return fmt.Errorf("apply external MariaDB Service/Endpoints manifest: %w\nOutput: %s", err, string(output))
	}

	_ = s.insertEvent(clusterID, "kubernetes-manifest", "success", "External MariaDB Service/Endpoints manifest applied", combined)
	return nil
}

func (s *Service) applyKubernetesAppDatabaseConfigAfterBootstrap(clusterID int64) error {
	cluster, err := s.Get(clusterID)
	if err != nil {
		return err
	}
	secret, err := s.loadDatabaseSecrets(clusterID)
	if err != nil {
		return fmt.Errorf("load database app secret for Kubernetes app config: %w", err)
	}
	return s.applyKubernetesAppDatabaseConfig(clusterID, cluster.HAProxyNamespace, cluster.HAProxyServiceName, cluster.DatabaseName, cluster.AppUsername, secret.AppPassword)
}

func (s *Service) applyKubernetesAppDatabaseConfig(clusterID int64, namespace, serviceName, databaseName, appUsername, appPassword string) error {
	namespace = nonEmpty(namespace, "pscs")
	serviceName = nonEmpty(serviceName, "external-mariadb")
	databaseName = nonEmpty(databaseName, "pscs")
	appUsername = nonEmpty(appUsername, "pscs")

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return err
	}

	configPatch, err := json.Marshal(map[string]any{
		"data": map[string]string{
			"DB_HOST": serviceName,
			"DB_PORT": "3306",
			"DB_NAME": databaseName,
		},
	})
	if err != nil {
		return fmt.Errorf("build configmap patch: %w", err)
	}
	secretPatch, err := json.Marshal(map[string]any{
		"data": map[string]string{
			"DB_USER": base64.StdEncoding.EncodeToString([]byte(appUsername)),
			"DB_PASS": base64.StdEncoding.EncodeToString([]byte(appPassword)),
		},
	})
	if err != nil {
		return fmt.Errorf("build secret patch: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = s.insertEvent(clusterID, "kubernetes-app-config", "info", "Updating app DB ConfigMap/Secret and restarting workloads", fmt.Sprintf("namespace=%s service=%s database=%s user=%s", namespace, serviceName, databaseName, appUsername))

	outputs := make([]string, 0, 5)
	commands := [][]string{
		{"--kubeconfig", kubeconfigPath, "-n", namespace, "patch", "configmap", "pscs-config", "--type", "merge", "-p", string(configPatch)},
		{"--kubeconfig", kubeconfigPath, "-n", namespace, "patch", "secret", "pscs-secrets", "--type", "merge", "-p", string(secretPatch)},
		{"--kubeconfig", kubeconfigPath, "-n", namespace, "rollout", "restart", "deployment/backend", "deployment/gateway"},
		{"--kubeconfig", kubeconfigPath, "-n", namespace, "rollout", "status", "deployment/backend", "--timeout=300s"},
		{"--kubeconfig", kubeconfigPath, "-n", namespace, "rollout", "status", "deployment/gateway", "--timeout=300s"},
	}
	for _, args := range commands {
		output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
		outputs = append(outputs, fmt.Sprintf("$ kubectl %s\n%s", strings.Join(redactKubectlArgs(args), " "), string(output)))
		if err != nil {
			combined := strings.Join(outputs, "\n")
			return fmt.Errorf("kubectl %s failed: %w\nOutput: %s", strings.Join(redactKubectlArgs(args), " "), err, combined)
		}
	}

	combined := strings.Join(outputs, "\n")
	_ = s.insertEvent(clusterID, "kubernetes-app-config", "success", "Kubernetes app now points to MariaDB Galera cluster", combined)
	return nil
}

func redactKubectlArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "-p" {
			out[i+1] = "<redacted-json-patch>"
		}
	}
	return out
}

func (s *Service) kubeconfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("KUBECONFIG")); envPath != "" {
		return envPath
	}
	return "/app/backend/.kube/config"
}

func (s *Service) resolveKubeconfigPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("kubeconfig file not found at %s: %w", path, err)
		}
		return "", fmt.Errorf("failed to stat kubeconfig path %s: %w", path, err)
	}
	if info.IsDir() {
		candidate := filepath.Join(path, "config")
		candidateInfo, candidateErr := os.Stat(candidate)
		if candidateErr == nil && !candidateInfo.IsDir() {
			return candidate, nil
		}
		return "", fmt.Errorf("kubeconfig path %s is a directory; mount the kubeconfig file, not a directory", path)
	}
	return path, nil
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
	return buildKubernetesEndpointManifest(namespace, serviceName, nodes) + fmt.Sprintf(`---
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
`, namespace, serviceName, databaseName, appUser)
}

func buildKubernetesEndpointManifest(namespace, serviceName string, nodes []NodeRecord) string {
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
`, serviceName, namespace, serviceName, namespace, addresses.String())
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
  gather_facts: false
  any_errors_fatal: true
  vars:
    galera_cluster_name: {{ .ClusterName | printf "%q" }}
    galera_cluster_address: {{ .ClusterAddress | printf "%q" }}
    sst_user: {{ .SSTUsername | printf "%q" }}
    sst_password: {{ .SSTPassword | printf "%q" }}
  pre_tasks:
    - name: Wait for SSH connectivity on every database node
      wait_for_connection:
        connect_timeout: 5
        sleep: 10
        timeout: 600

    - name: Gather facts after SSH is reachable
      setup:

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
      register: qemu_guest_agent_service
      failed_when: false

    - name: Record qemu guest agent startup warning
      debug:
        msg: "qemu-guest-agent did not start; continuing because Galera bootstrap does not require it. Check VM guest-agent device settings if Proxmox guest status is needed."
      when: qemu_guest_agent_service is failed

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
