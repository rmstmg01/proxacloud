package vps

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/secrets"
	"k8s-proxmox-manager/backend/internal/sshkeys"
	"k8s-proxmox-manager/backend/internal/terraform"
)

const (
	IPModeDHCP   = "dhcp"
	IPModeManual = "manual"
	IPModeAuto   = "auto"

	defaultCloudImageDir = "/var/lib/vz/template/cloud-images"
)

var (
	ErrVPSNotFound = errors.New("vps not found")
	ErrVPSExists   = errors.New("vps already exists")
)

type NetworkStore interface {
	GetByID(id int64) (networks.Profile, error)
	GetReservedIPsByNetworkProfileID(networkProfileID int64) ([]networks.ReservedIP, error)
}

type NodeInventory interface {
	ListEnabledNodes() ([]proxmox.Node, error)
}

type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	ControlVMByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, action string) (string, error)
	GetVMStatusByName(cred proxmox.Credential, proxmoxNodeName string, vmName string) (string, error)
}

type SSHKeyPairStore interface {
	GetByID(id int64) (sshkeys.KeyPair, error)
}

type Service struct {
	db                 *sql.DB
	networks           NetworkStore
	nodes              NodeInventory
	proxmoxCredentials ProxmoxCredentialStore
	sshKeys            SSHKeyPairStore
	secrets            secrets.Store
	terraformRunner    *terraform.Runner
	autoApply          bool
}

type CloudInitImage struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	ImagePath        string    `json:"image_path"`
	OSType           string    `json:"os_type"`
	Description      string    `json:"description"`
	IsDefault        bool      `json:"is_default"`
	SSHUser          string    `json:"ssh_user"`
	DefaultCPU       int       `json:"default_cpu"`
	DefaultMemoryMB  int       `json:"default_memory_mb"`
	DefaultDiskGB    int       `json:"default_disk_gb"`
	SourcePath       string    `json:"source_path"`
	DetectedNodeName string    `json:"detected_node_name"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateCloudInitImageRequest struct {
	Name             string `json:"name"`
	ImagePath        string `json:"image_path"`
	OSType           string `json:"os_type"`
	Description      string `json:"description"`
	IsDefault        bool   `json:"is_default"`
	SSHUser          string `json:"ssh_user"`
	DefaultCPU       int    `json:"default_cpu"`
	DefaultMemoryMB  int    `json:"default_memory_mb"`
	DefaultDiskGB    int    `json:"default_disk_gb"`
	SourcePath       string `json:"source_path"`
	DetectedNodeName string `json:"detected_node_name"`
}

type Instance struct {
	ID                  int64     `json:"id"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	ProxmoxCredentialID int64     `json:"proxmox_credential_id"`
	ProxmoxNodeName     string    `json:"proxmox_node_name"`
	NetworkProfileID    int64     `json:"network_profile_id"`
	NetworkName         string    `json:"network_name,omitempty"`
	CloudInitImageID    int64     `json:"cloud_init_image_id"`
	CloudInitImagePath  string    `json:"cloud_init_image_path"`
	SSHKeyPairID        int64     `json:"ssh_key_pair_id"`
	VMName              string    `json:"vm_name"`
	IPMode              string    `json:"ip_mode"`
	IPAddress           string    `json:"ip_address"`
	CPU                 int       `json:"cpu"`
	MemoryMB            int       `json:"memory_mb"`
	DiskGB              int       `json:"disk_gb"`
	SSHUser             string    `json:"ssh_user"`
	TerraformWorkDir    string    `json:"terraform_work_dir"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Event struct {
	ID        int64     `json:"id"`
	VPSID     int64     `json:"vps_id"`
	Action    string    `json:"action"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateRequest struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	ProxmoxCredentialID int64  `json:"proxmox_credential_id"`
	ProxmoxNodeName     string `json:"proxmox_node_name"`
	NetworkProfileID    int64  `json:"network_profile_id"`
	CloudInitImageID    int64  `json:"cloud_init_image_id"`
	CloudInitImagePath  string `json:"cloud_init_image_path"`
	SSHKeyPairID        int64  `json:"ssh_key_pair_id"`
	IPMode              string `json:"ip_mode"`
	IPAddress           string `json:"ip_address"`
	CPU                 int    `json:"cpu"`
	MemoryMB            int    `json:"memory_mb"`
	DiskGB              int    `json:"disk_gb"`
	SSHUser             string `json:"ssh_user"`
	RootPassword        string `json:"root_password"`
	Apply               bool   `json:"apply"`
}

type CreateResponse struct {
	VPSID            int64    `json:"vps_id"`
	Status           string   `json:"status"`
	TerraformWorkDir string   `json:"terraform_work_dir"`
	TerraformOutput  string   `json:"terraform_output,omitempty"`
	Warnings         []string `json:"warnings"`
}

type ActionResponse struct {
	VPSID    int64    `json:"vps_id"`
	Action   string   `json:"action"`
	Status   string   `json:"status"`
	Output   string   `json:"output,omitempty"`
	Warnings []string `json:"warnings"`
}

func NewService(db *sql.DB, networkStore NetworkStore, nodeInventory NodeInventory, proxmoxCredentials ProxmoxCredentialStore, sshKeys SSHKeyPairStore, secretStore secrets.Store, terraformRunner *terraform.Runner, autoApply bool) *Service {
	return &Service{
		db:                 db,
		networks:           networkStore,
		nodes:              nodeInventory,
		proxmoxCredentials: proxmoxCredentials,
		sshKeys:            sshKeys,
		secrets:            secretStore,
		terraformRunner:    terraformRunner,
		autoApply:          autoApply,
	}
}

func (s *Service) List() ([]Instance, error) {
	rows, err := s.db.Query(`SELECT v.id, v.name, v.description, v.proxmox_credential_id, v.proxmox_node_name, v.network_profile_id, COALESCE(n.name, ''), COALESCE(v.cloud_init_image_id, 0), v.cloud_init_image_path, COALESCE(v.ssh_key_pair_id, 0), v.vm_name, v.ip_mode, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.ssh_user, v.terraform_work_dir, v.status, v.created_at, v.updated_at FROM vps_instances v LEFT JOIN network_profiles n ON n.id = v.network_profile_id ORDER BY v.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list vps instances: %w", err)
	}
	defer rows.Close()

	out := []Instance{}
	for rows.Next() {
		var rec Instance
		if err := scanInstance(rows, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) Get(id int64) (Instance, error) {
	var rec Instance
	err := s.db.QueryRow(`SELECT v.id, v.name, v.description, v.proxmox_credential_id, v.proxmox_node_name, v.network_profile_id, COALESCE(n.name, ''), COALESCE(v.cloud_init_image_id, 0), v.cloud_init_image_path, COALESCE(v.ssh_key_pair_id, 0), v.vm_name, v.ip_mode, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.ssh_user, v.terraform_work_dir, v.status, v.created_at, v.updated_at FROM vps_instances v LEFT JOIN network_profiles n ON n.id = v.network_profile_id WHERE v.id = $1`, id).Scan(
		&rec.ID,
		&rec.Name,
		&rec.Description,
		&rec.ProxmoxCredentialID,
		&rec.ProxmoxNodeName,
		&rec.NetworkProfileID,
		&rec.NetworkName,
		&rec.CloudInitImageID,
		&rec.CloudInitImagePath,
		&rec.SSHKeyPairID,
		&rec.VMName,
		&rec.IPMode,
		&rec.IPAddress,
		&rec.CPU,
		&rec.MemoryMB,
		&rec.DiskGB,
		&rec.SSHUser,
		&rec.TerraformWorkDir,
		&rec.Status,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, ErrVPSNotFound
	}
	if err != nil {
		return Instance{}, fmt.Errorf("get vps: %w", err)
	}
	return rec, nil
}

func (s *Service) Create(req CreateRequest) (CreateResponse, error) {
	if err := s.validateCreateRequest(&req); err != nil {
		return CreateResponse{}, err
	}

	network, err := s.networks.GetByID(req.NetworkProfileID)
	if err != nil {
		return CreateResponse{}, fmt.Errorf("load network profile: %w", err)
	}

	if err := s.validateNode(req.ProxmoxCredentialID, req.ProxmoxNodeName); err != nil {
		return CreateResponse{}, err
	}

	image, err := s.resolveImage(req.CloudInitImageID, req.CloudInitImagePath)
	if err != nil {
		return CreateResponse{}, err
	}
	req.CloudInitImageID = image.ID
	req.CloudInitImagePath = image.ImagePath

	sshUser := strings.TrimSpace(req.SSHUser)
	if sshUser == "" {
		sshUser = strings.TrimSpace(image.SSHUser)
	}
	if sshUser == "" {
		sshUser = "ubuntu"
	}
	sshPublicKey := ""
	if req.SSHKeyPairID > 0 {
		pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
		if err != nil {
			return CreateResponse{}, fmt.Errorf("load ssh key pair: %w", err)
		}
		sshPublicKey = strings.TrimSpace(pair.PublicKey)
	}

	rootPassword := strings.TrimSpace(req.RootPassword)
	if rootPassword == "" && sshPublicKey == "" {
		return CreateResponse{}, errors.New("select an ssh key or set a root password")
	}
	if strings.ContainsAny(rootPassword, "\r\n") {
		return CreateResponse{}, errors.New("root password cannot contain newlines")
	}

	ipAddress, err := s.resolveIPAddress(req)
	if err != nil {
		return CreateResponse{}, err
	}
	req.IPAddress = ipAddress

	workDir := s.terraformRunner.WorkspaceDirForKey("vps-" + sanitizeName(req.Name))
	rootPasswordRef := ""
	if rootPassword != "" {
		ref, err := s.secrets.Put(context.Background(), "vps/"+sanitizeName(req.Name), "root-password", []byte(rootPassword))
		if err != nil {
			return CreateResponse{}, fmt.Errorf("store root password: %w", err)
		}
		rootPasswordRef = ref
	}

	var vpsID int64
	err = s.db.QueryRow(
		`INSERT INTO vps_instances (name, description, proxmox_credential_id, proxmox_node_name, network_profile_id, cloud_init_image_id, cloud_init_image_path, ssh_key_pair_id, vm_name, ip_mode, ip_address, cpu, memory_mb, disk_gb, ssh_user, root_password_secret_ref, terraform_work_dir, status)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), $7, NULLIF($8, 0), $9, $10, $11, $12, $13, $14, $15, $16, $17, 'created')
		 RETURNING id`,
		req.Name,
		req.Description,
		req.ProxmoxCredentialID,
		req.ProxmoxNodeName,
		req.NetworkProfileID,
		req.CloudInitImageID,
		req.CloudInitImagePath,
		req.SSHKeyPairID,
		req.Name,
		req.IPMode,
		req.IPAddress,
		req.CPU,
		req.MemoryMB,
		req.DiskGB,
		sshUser,
		rootPasswordRef,
		workDir,
	).Scan(&vpsID)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			return CreateResponse{}, ErrVPSExists
		}
		return CreateResponse{}, fmt.Errorf("insert vps: %w", err)
	}

	spec := terraform.StandaloneVPSSpec{
		WorkspaceKey:    "vps-" + req.Name,
		Name:            req.Name,
		ProxmoxNodeName: req.ProxmoxNodeName,
		CPU:             req.CPU,
		MemoryMB:        req.MemoryMB,
		DiskGB:          req.DiskGB,
		NetworkCIDR:     network.CIDR,
		Gateway:         network.Gateway,
		DNS:             network.DNS,
		Bridge:          network.Bridge,
		VLAN:            network.VLAN,
		CloudInitImage:  req.CloudInitImagePath,
		SSHPublicKey:    sshPublicKey,
		SSHUser:         sshUser,
		RootPassword:    rootPassword,
		IPAddress:       req.IPAddress,
	}
	workDir, err = s.terraformRunner.PrepareStandaloneVPSWorkspace(spec)
	if err != nil {
		_ = s.updateStatus(vpsID, "failed")
		_ = s.insertEvent(vpsID, "terraform-prepare", "error", "Terraform workspace generation failed", err.Error())
		return CreateResponse{}, err
	}
	_, _ = s.db.Exec(`UPDATE vps_instances SET terraform_work_dir = $2, updated_at = NOW() WHERE id = $1`, vpsID, workDir)
	_ = s.insertEvent(vpsID, "terraform-prepare", "success", "Terraform workspace generated", workDir)

	resp := CreateResponse{VPSID: vpsID, Status: "created", TerraformWorkDir: workDir, Warnings: []string{}}
	if req.Apply || s.autoApply {
		action, err := s.Deploy(vpsID)
		resp.Status = action.Status
		resp.TerraformOutput = action.Output
		resp.Warnings = action.Warnings
		if err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func (s *Service) Deploy(id int64) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}

	_ = s.updateStatus(id, "deploying")
	_ = s.insertEvent(id, "terraform-apply", "info", "Starting Terraform apply for VPS", rec.TerraformWorkDir)
	result, err := s.terraformRunner.ApplyStandaloneVPS(rec.TerraformWorkDir, terraformAuthFromCredential(cred))
	if err != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-apply", "error", "Terraform apply failed", result.Output+"\n"+err.Error())
		return ActionResponse{VPSID: id, Action: "deploy", Status: "failed", Output: result.Output, Warnings: []string{}}, err
	}
	_ = s.updateStatus(id, "running")
	_ = s.insertEvent(id, "terraform-apply", "success", "VPS deployed successfully", result.Output)
	return ActionResponse{VPSID: id, Action: "deploy", Status: "running", Output: result.Output, Warnings: []string{}}, nil
}

func (s *Service) Redeploy(id int64) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	if strings.TrimSpace(rec.TerraformWorkDir) == "" {
		return ActionResponse{}, errors.New("terraform workspace is not configured for this VPS")
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	auth := terraformAuthFromCredential(cred)

	_ = s.updateStatus(id, "redeploying")
	_ = s.insertEvent(id, "redeploy", "info", "Starting VPS OS reinstall", rec.TerraformWorkDir)

	destroyResult, destroyErr := s.terraformRunner.DestroyStandaloneVPS(rec.TerraformWorkDir, auth)
	if destroyErr != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-destroy", "error", "Terraform destroy failed during VPS redeploy", destroyResult.Output+"\n"+destroyErr.Error())
		return ActionResponse{VPSID: id, Action: "redeploy", Status: "failed", Output: destroyResult.Output, Warnings: []string{}}, destroyErr
	}
	_ = s.insertEvent(id, "terraform-destroy", "success", "Existing VPS infrastructure destroyed for redeploy", destroyResult.Output)

	applyResult, applyErr := s.terraformRunner.ApplyStandaloneVPS(rec.TerraformWorkDir, auth)
	output := strings.TrimSpace(destroyResult.Output + "\n" + applyResult.Output)
	if applyErr != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-apply", "error", "Terraform apply failed during VPS redeploy", applyResult.Output+"\n"+applyErr.Error())
		return ActionResponse{VPSID: id, Action: "redeploy", Status: "failed", Output: output, Warnings: []string{}}, applyErr
	}

	_ = s.updateStatus(id, "running")
	_ = s.insertEvent(id, "terraform-apply", "success", "VPS redeployed successfully", applyResult.Output)
	return ActionResponse{VPSID: id, Action: "redeploy", Status: "running", Output: output, Warnings: []string{}}, nil
}

func (s *Service) PowerAction(id int64, action string) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	action = strings.TrimSpace(strings.ToLower(action))
	if action != "start" && action != "restart" && action != "shutdown" {
		return ActionResponse{}, fmt.Errorf("unsupported vps action %q", action)
	}
	proxmoxAction := action
	if action == "restart" {
		proxmoxAction = "reboot"
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	actual, err := s.proxmoxCredentials.ControlVMByName(cred, rec.ProxmoxNodeName, rec.VMName, proxmoxAction)
	if err != nil {
		_ = s.insertEvent(id, action, "error", "Proxmox power action failed", err.Error())
		return ActionResponse{VPSID: id, Action: action, Status: rec.Status, Warnings: []string{err.Error()}}, err
	}
	newStatus := "running"
	if action == "shutdown" {
		newStatus = "stopped"
	}
	_ = s.updateStatus(id, newStatus)
	_ = s.insertEvent(id, action, "success", "Proxmox power action completed", actual)
	return ActionResponse{VPSID: id, Action: action, Status: newStatus, Output: actual, Warnings: []string{}}, nil
}

func (s *Service) RefreshStatus(id int64) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	status, err := s.proxmoxCredentials.GetVMStatusByName(cred, rec.ProxmoxNodeName, rec.VMName)
	if err != nil {
		return ActionResponse{}, err
	}
	mapped := status
	if status == "running" {
		mapped = "running"
	} else if status == "stopped" {
		mapped = "stopped"
	}
	_ = s.updateStatus(id, mapped)
	return ActionResponse{VPSID: id, Action: "refresh-status", Status: mapped, Output: status, Warnings: []string{}}, nil
}

func (s *Service) Delete(id int64, confirmationName string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmationName) != rec.Name {
		return errors.New("vps name confirmation does not match")
	}
	if strings.TrimSpace(rec.TerraformWorkDir) != "" {
		cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
		if err != nil {
			return fmt.Errorf("load proxmox credentials: %w", err)
		}
		result, destroyErr := s.terraformRunner.DestroyStandaloneVPS(rec.TerraformWorkDir, terraformAuthFromCredential(cred))
		if destroyErr != nil {
			_ = s.insertEvent(id, "terraform-destroy", "error", "Terraform destroy failed", result.Output+"\n"+destroyErr.Error())
			return destroyErr
		}
		_ = s.insertEvent(id, "terraform-destroy", "success", "VPS infrastructure destroyed", result.Output)
	}
	if _, err := s.db.Exec(`DELETE FROM vps_instances WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete vps: %w", err)
	}
	if strings.TrimSpace(rec.TerraformWorkDir) != "" {
		_ = os.RemoveAll(rec.TerraformWorkDir)
	}
	return nil
}

func (s *Service) ListEvents(id int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, vps_id, action, level, message, output, created_at FROM vps_events WHERE vps_id = $1 ORDER BY created_at DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list vps events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.VPSID, &event.Action, &event.Level, &event.Message, &event.Output, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (s *Service) ListImages() ([]CloudInitImage, error) {
	rows, err := s.db.Query(`SELECT id, name, image_path, os_type, description, is_default, ssh_user, default_cpu, default_memory_mb, default_disk_gb, source_path, detected_node_name, created_at, updated_at FROM vps_cloud_init_images ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list cloud-init images: %w", err)
	}
	defer rows.Close()
	out := []CloudInitImage{}
	for rows.Next() {
		var image CloudInitImage
		if err := scanCloudInitImage(rows, &image); err != nil {
			return nil, err
		}
		out = append(out, image)
	}
	return out, nil
}

func (s *Service) CreateImage(req CreateCloudInitImageRequest) (CloudInitImage, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.ImagePath = strings.TrimSpace(req.ImagePath)
	req.OSType = strings.TrimSpace(req.OSType)
	req.Description = strings.TrimSpace(req.Description)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.SourcePath = strings.TrimSpace(req.SourcePath)
	req.DetectedNodeName = strings.TrimSpace(req.DetectedNodeName)
	if req.Name == "" {
		return CloudInitImage{}, errors.New("name is required")
	}
	if req.ImagePath == "" {
		return CloudInitImage{}, errors.New("image_path is required")
	}
	if req.OSType == "" {
		req.OSType = "ubuntu"
	}
	if req.SSHUser == "" {
		req.SSHUser = defaultSSHUserForOS(req.OSType)
	}
	if req.DefaultCPU <= 0 {
		req.DefaultCPU = 1
	}
	if req.DefaultMemoryMB <= 0 {
		req.DefaultMemoryMB = 1024
	}
	if req.DefaultDiskGB <= 0 {
		req.DefaultDiskGB = 20
	}
	if req.IsDefault {
		if _, err := s.db.Exec(`UPDATE vps_cloud_init_images SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return CloudInitImage{}, fmt.Errorf("clear default cloud-init image: %w", err)
		}
	}
	var image CloudInitImage
	err := s.db.QueryRow(
		`INSERT INTO vps_cloud_init_images (name, image_path, os_type, description, is_default, ssh_user, default_cpu, default_memory_mb, default_disk_gb, source_path, detected_node_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (image_path) DO UPDATE SET name = EXCLUDED.name, os_type = EXCLUDED.os_type, description = EXCLUDED.description, is_default = EXCLUDED.is_default, ssh_user = EXCLUDED.ssh_user, default_cpu = EXCLUDED.default_cpu, default_memory_mb = EXCLUDED.default_memory_mb, default_disk_gb = EXCLUDED.default_disk_gb, source_path = EXCLUDED.source_path, detected_node_name = EXCLUDED.detected_node_name, updated_at = NOW()
		 RETURNING id, name, image_path, os_type, description, is_default, ssh_user, default_cpu, default_memory_mb, default_disk_gb, source_path, detected_node_name, created_at, updated_at`,
		req.Name, req.ImagePath, req.OSType, req.Description, req.IsDefault, req.SSHUser, req.DefaultCPU, req.DefaultMemoryMB, req.DefaultDiskGB, req.SourcePath, req.DetectedNodeName,
	).Scan(&image.ID, &image.Name, &image.ImagePath, &image.OSType, &image.Description, &image.IsDefault, &image.SSHUser, &image.DefaultCPU, &image.DefaultMemoryMB, &image.DefaultDiskGB, &image.SourcePath, &image.DetectedNodeName, &image.CreatedAt, &image.UpdatedAt)
	if err != nil {
		return CloudInitImage{}, fmt.Errorf("create cloud-init image: %w", err)
	}
	return image, nil
}

func (s *Service) DiscoverImages(nodeName string) ([]CloudInitImage, error) {
	entries, err := os.ReadDir(defaultCloudImageDir)
	if err != nil {
		return nil, fmt.Errorf("scan cloud image directory %s: %w", defaultCloudImageDir, err)
	}
	discovered := make([]CloudInitImage, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".qcow2" && ext != ".img" && ext != ".raw" {
			continue
		}
		path := filepath.Join(defaultCloudImageDir, name)
		meta := inferCloudImageMetadata(name)
		image, err := s.CreateImage(CreateCloudInitImageRequest{
			Name:             meta.Name,
			ImagePath:        path,
			OSType:           meta.OSType,
			Description:      "Discovered from " + defaultCloudImageDir,
			IsDefault:        false,
			SSHUser:          meta.SSHUser,
			DefaultCPU:       meta.DefaultCPU,
			DefaultMemoryMB:  meta.DefaultMemoryMB,
			DefaultDiskGB:    meta.DefaultDiskGB,
			SourcePath:       path,
			DetectedNodeName: strings.TrimSpace(nodeName),
		})
		if err != nil {
			return nil, err
		}
		discovered = append(discovered, image)
	}
	sort.Slice(discovered, func(i, j int) bool {
		return strings.ToLower(discovered[i].Name) < strings.ToLower(discovered[j].Name)
	})
	return discovered, nil
}

func (s *Service) validateCreateRequest(req *CreateRequest) error {
	req.Name = sanitizeName(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.ProxmoxNodeName = strings.TrimSpace(req.ProxmoxNodeName)
	req.CloudInitImagePath = strings.TrimSpace(req.CloudInitImagePath)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.IPMode = strings.TrimSpace(strings.ToLower(req.IPMode))
	req.IPAddress = strings.TrimSpace(req.IPAddress)
	if req.IPMode == "" {
		req.IPMode = IPModeDHCP
	}
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.ProxmoxCredentialID <= 0 {
		return errors.New("proxmox_credential_id is required")
	}
	if req.ProxmoxNodeName == "" {
		return errors.New("proxmox_node_name is required")
	}
	if req.NetworkProfileID <= 0 {
		return errors.New("network_profile_id is required")
	}
	if req.IPMode != IPModeDHCP && req.IPMode != IPModeManual && req.IPMode != IPModeAuto {
		return errors.New("ip_mode must be dhcp, manual, or auto")
	}
	if req.IPMode == IPModeManual && req.IPAddress == "" {
		return errors.New("ip_address is required for manual ip mode")
	}
	if req.CPU <= 0 {
		return errors.New("cpu must be greater than 0")
	}
	if req.MemoryMB < 256 {
		return errors.New("memory_mb must be at least 256")
	}
	if req.DiskGB < 5 {
		return errors.New("disk_gb must be at least 5")
	}
	return nil
}

func (s *Service) validateNode(credentialID int64, nodeName string) error {
	nodes, err := s.nodes.ListEnabledNodes()
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.CredentialID == credentialID && strings.EqualFold(node.NodeName, nodeName) {
			return nil
		}
	}
	return fmt.Errorf("proxmox node %q is not enabled for the selected credential", nodeName)
}

func (s *Service) resolveImage(id int64, imagePath string) (CloudInitImage, error) {
	if id > 0 {
		var image CloudInitImage
		err := s.db.QueryRow(`SELECT id, name, image_path, os_type, description, is_default, ssh_user, default_cpu, default_memory_mb, default_disk_gb, source_path, detected_node_name, created_at, updated_at FROM vps_cloud_init_images WHERE id = $1`, id).
			Scan(&image.ID, &image.Name, &image.ImagePath, &image.OSType, &image.Description, &image.IsDefault, &image.SSHUser, &image.DefaultCPU, &image.DefaultMemoryMB, &image.DefaultDiskGB, &image.SourcePath, &image.DetectedNodeName, &image.CreatedAt, &image.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return CloudInitImage{}, errors.New("selected cloud-init image not found")
		}
		if err != nil {
			return CloudInitImage{}, fmt.Errorf("load cloud-init image: %w", err)
		}
		return image, nil
	}
	if strings.TrimSpace(imagePath) == "" {
		return CloudInitImage{}, errors.New("select a cloud-init image or provide image path")
	}
	meta := inferCloudImageMetadata(filepath.Base(imagePath))
	return CloudInitImage{ImagePath: strings.TrimSpace(imagePath), SSHUser: meta.SSHUser, DefaultCPU: meta.DefaultCPU, DefaultMemoryMB: meta.DefaultMemoryMB, DefaultDiskGB: meta.DefaultDiskGB}, nil
}

func (s *Service) resolveIPAddress(req CreateRequest) (string, error) {
	if req.IPMode == IPModeDHCP {
		return "", nil
	}
	network, err := s.networks.GetByID(req.NetworkProfileID)
	if err != nil {
		return "", err
	}
	if req.IPMode == IPModeManual {
		return validateIPAddress(req.IPAddress, network)
	}
	return s.nextAvailableIP(network)
}

func (s *Service) nextAvailableIP(profile networks.Profile) (string, error) {
	_, cidrNet, err := net.ParseCIDR(profile.CIDR)
	if err != nil {
		return "", errors.New("network profile cidr is invalid")
	}
	used := map[string]struct{}{}
	used[strings.TrimSpace(profile.Gateway)] = struct{}{}
	reserved, err := s.networks.GetReservedIPsByNetworkProfileID(profile.ID)
	if err == nil {
		for _, r := range reserved {
			used[strings.TrimSpace(r.IPAddress)] = struct{}{}
		}
	}
	rows, err := s.db.Query(`SELECT ip_address FROM vps_instances WHERE network_profile_id = $1 AND ip_address <> ''`, profile.ID)
	if err != nil {
		return "", fmt.Errorf("query vps ips: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ip string
		if rows.Scan(&ip) == nil {
			used[strings.TrimSpace(ip)] = struct{}{}
		}
	}

	base := binary.BigEndian.Uint32(cidrNet.IP.To4())
	mask := binary.BigEndian.Uint32(cidrNet.Mask)
	start := (base & mask) + 10
	end := (base & mask) | ^mask
	for value := start; value < end; value++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, value)
		if !cidrNet.Contains(ip) || isNetworkOrBroadcast(cidrNet, ip) {
			continue
		}
		candidate := ip.String()
		if _, exists := used[candidate]; exists {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("no free IP found in network %s", profile.CIDR)
}

func validateIPAddress(value string, profile networks.Profile) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(value)).To4()
	if ip == nil {
		return "", errors.New("ip_address is invalid")
	}
	_, cidrNet, err := net.ParseCIDR(profile.CIDR)
	if err != nil {
		return "", errors.New("network profile cidr is invalid")
	}
	if !cidrNet.Contains(ip) {
		return "", fmt.Errorf("ip_address must be inside network cidr %s", profile.CIDR)
	}
	if ip.Equal(net.ParseIP(profile.Gateway).To4()) || isNetworkOrBroadcast(cidrNet, ip) {
		return "", errors.New("ip_address cannot be the gateway, network, or broadcast address")
	}
	return ip.String(), nil
}

func isNetworkOrBroadcast(cidr *net.IPNet, ip net.IP) bool {
	networkIP := cidr.IP.To4()
	ipv4 := ip.To4()
	if networkIP == nil || ipv4 == nil {
		return false
	}
	if networkIP.Equal(ipv4) {
		return true
	}
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = networkIP[i] | ^cidr.Mask[i]
	}
	return broadcast.Equal(ipv4)
}

func (s *Service) updateStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE vps_instances SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

func (s *Service) insertEvent(id int64, action, level, message, output string) error {
	_, err := s.db.Exec(`INSERT INTO vps_events (vps_id, action, level, message, output) VALUES ($1, $2, $3, $4, $5)`, id, action, level, message, output)
	return err
}

func scanInstance(rows interface {
	Scan(dest ...any) error
}, rec *Instance) error {
	return rows.Scan(
		&rec.ID,
		&rec.Name,
		&rec.Description,
		&rec.ProxmoxCredentialID,
		&rec.ProxmoxNodeName,
		&rec.NetworkProfileID,
		&rec.NetworkName,
		&rec.CloudInitImageID,
		&rec.CloudInitImagePath,
		&rec.SSHKeyPairID,
		&rec.VMName,
		&rec.IPMode,
		&rec.IPAddress,
		&rec.CPU,
		&rec.MemoryMB,
		&rec.DiskGB,
		&rec.SSHUser,
		&rec.TerraformWorkDir,
		&rec.Status,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
}

func scanCloudInitImage(rows interface {
	Scan(dest ...any) error
}, image *CloudInitImage) error {
	return rows.Scan(
		&image.ID,
		&image.Name,
		&image.ImagePath,
		&image.OSType,
		&image.Description,
		&image.IsDefault,
		&image.SSHUser,
		&image.DefaultCPU,
		&image.DefaultMemoryMB,
		&image.DefaultDiskGB,
		&image.SourcePath,
		&image.DetectedNodeName,
		&image.CreatedAt,
		&image.UpdatedAt,
	)
}

type inferredCloudImage struct {
	Name            string
	OSType          string
	SSHUser         string
	DefaultCPU      int
	DefaultMemoryMB int
	DefaultDiskGB   int
}

func inferCloudImageMetadata(filename string) inferredCloudImage {
	lower := strings.ToLower(filename)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	out := inferredCloudImage{
		Name:            strings.NewReplacer("_", " ", "-", " ").Replace(name),
		OSType:          "generic",
		SSHUser:         "cloud-user",
		DefaultCPU:      2,
		DefaultMemoryMB: 2048,
		DefaultDiskGB:   20,
	}
	switch {
	case strings.Contains(lower, "almalinux"):
		out.OSType = "almalinux"
		out.SSHUser = "almalinux"
	case strings.Contains(lower, "ubuntu"):
		out.OSType = "ubuntu"
		out.SSHUser = "ubuntu"
	case strings.Contains(lower, "debian"):
		out.OSType = "debian"
		out.SSHUser = "debian"
	case strings.Contains(lower, "rocky"):
		out.OSType = "rocky"
		out.SSHUser = "rocky"
	case strings.Contains(lower, "centos"):
		out.OSType = "centos"
		out.SSHUser = "centos"
	case strings.Contains(lower, "fedora"):
		out.OSType = "fedora"
		out.SSHUser = "fedora"
	}
	return out
}

func defaultSSHUserForOS(osType string) string {
	switch strings.ToLower(strings.TrimSpace(osType)) {
	case "almalinux":
		return "almalinux"
	case "ubuntu":
		return "ubuntu"
	case "debian":
		return "debian"
	case "rocky":
		return "rocky"
	case "centos", "fedora", "generic":
		return "cloud-user"
	default:
		return "cloud-user"
	}
}

func terraformAuthFromCredential(cred proxmox.Credential) terraform.ProxmoxAuth {
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
	if strings.TrimSpace(auth.APITokenID) != "" && strings.TrimSpace(auth.APITokenSecret) != "" {
		auth.Username = ""
		auth.Password = ""
	}
	return auth
}

func sshUserFromCredential(cred proxmox.Credential) string {
	user := strings.TrimSpace(cred.Username)
	if user == "" {
		return "root"
	}
	if at := strings.Index(user, "@"); at > 0 {
		return user[:at]
	}
	return user
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	previousDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			previousDash = false
			continue
		}
		if !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
