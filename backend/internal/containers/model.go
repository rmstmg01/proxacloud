package containers

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"proxacloud/backend/internal/networks"
	"proxacloud/backend/internal/proxmox"
	"proxacloud/backend/internal/secrets"
	"proxacloud/backend/internal/sshkeys"
	"proxacloud/backend/internal/tenant"
	"proxacloud/backend/internal/terraform"
)

const (
	IPModeDHCP   = "dhcp"
	IPModeManual = "manual"
	IPModeAuto   = "auto"
)

var (
	ErrContainerNotFound = errors.New("container not found")
	ErrContainerExists   = errors.New("container already exists")
)

type NetworkStore interface {
	GetByID(id int64) (networks.Profile, error)
	GetReservedIPsByNetworkProfileID(networkProfileID int64) ([]networks.ReservedIP, error)
}

type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	ControlContainerByName(cred proxmox.Credential, proxmoxNodeName string, containerName string, action string) (string, error)
	GetContainerStatusByName(cred proxmox.Credential, proxmoxNodeName string, containerName string) (string, error)
}

type SSHKeyPairStore interface {
	GetByID(id int64) (sshkeys.KeyPair, error)
}

type Service struct {
	db                 *sql.DB
	networks           NetworkStore
	proxmoxCredentials ProxmoxCredentialStore
	sshKeys            SSHKeyPairStore
	secrets            secrets.Store
	terraformRunner    *terraform.Runner
	autoApply          bool
}

type Template struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	TemplatePath string    `json:"template_path"`
	OSType       string    `json:"os_type"`
	Description  string    `json:"description"`
	IsDefault    bool      `json:"is_default"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Instance struct {
	ID                  int64     `json:"id"`
	CustomerAccountID   int64     `json:"customer_account_id,omitempty"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	ProxmoxCredentialID int64     `json:"proxmox_credential_id"`
	ProxmoxNodeName     string    `json:"proxmox_node_name"`
	NetworkProfileID    int64     `json:"network_profile_id"`
	NetworkName         string    `json:"network_name,omitempty"`
	TemplateID          int64     `json:"template_id"`
	TemplatePath        string    `json:"template_path"`
	SSHKeyPairID        int64     `json:"ssh_key_pair_id"`
	ContainerName       string    `json:"container_name"`
	IPMode              string    `json:"ip_mode"`
	IPAddress           string    `json:"ip_address"`
	CPU                 int       `json:"cpu"`
	MemoryMB            int       `json:"memory_mb"`
	DiskGB              int       `json:"disk_gb"`
	Unprivileged        bool      `json:"unprivileged"`
	Nesting             bool      `json:"nesting"`
	TerraformWorkDir    string    `json:"terraform_work_dir"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Event struct {
	ID          int64     `json:"id"`
	ContainerID int64     `json:"container_id"`
	Action      string    `json:"action"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	Output      string    `json:"output"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateTemplateRequest struct {
	Name         string `json:"name"`
	TemplatePath string `json:"template_path"`
	OSType       string `json:"os_type"`
	Description  string `json:"description"`
	IsDefault    bool   `json:"is_default"`
}

type CreateRequest struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	ProxmoxCredentialID int64  `json:"proxmox_credential_id"`
	ProxmoxNodeName     string `json:"proxmox_node_name"`
	NetworkProfileID    int64  `json:"network_profile_id"`
	TemplateID          int64  `json:"template_id"`
	TemplatePath        string `json:"template_path"`
	SSHKeyPairID        int64  `json:"ssh_key_pair_id"`
	IPMode              string `json:"ip_mode"`
	IPAddress           string `json:"ip_address"`
	CPU                 int    `json:"cpu"`
	MemoryMB            int    `json:"memory_mb"`
	DiskGB              int    `json:"disk_gb"`
	RootPassword        string `json:"root_password"`
	Unprivileged        bool   `json:"unprivileged"`
	Nesting             bool   `json:"nesting"`
	Apply               bool   `json:"apply"`
	CustomerAccountID   int64  `json:"-"`
}

type CreateResponse struct {
	ContainerID      int64    `json:"container_id"`
	Status           string   `json:"status"`
	TerraformWorkDir string   `json:"terraform_work_dir"`
	TerraformOutput  string   `json:"terraform_output,omitempty"`
	Warnings         []string `json:"warnings"`
}

type ActionResponse struct {
	ContainerID int64    `json:"container_id"`
	Action      string   `json:"action"`
	Status      string   `json:"status"`
	Output      string   `json:"output,omitempty"`
	Warnings    []string `json:"warnings"`
}

func NewService(db *sql.DB, networkStore NetworkStore, proxmoxCredentials ProxmoxCredentialStore, sshKeys SSHKeyPairStore, secretStore secrets.Store, terraformRunner *terraform.Runner, autoApply bool) *Service {
	return &Service{db: db, networks: networkStore, proxmoxCredentials: proxmoxCredentials, sshKeys: sshKeys, secrets: secretStore, terraformRunner: terraformRunner, autoApply: autoApply}
}

func (s *Service) List(scope tenant.Scope) ([]Instance, error) {
	query := `SELECT c.id, COALESCE(c.customer_account_id, 0), c.name, c.description, c.proxmox_credential_id, c.proxmox_node_name, c.network_profile_id, COALESCE(n.name, ''), COALESCE(c.template_id, 0), c.template_path, COALESCE(c.ssh_key_pair_id, 0), c.container_name, c.ip_mode, c.ip_address, c.cpu, c.memory_mb, c.disk_gb, c.unprivileged, c.nesting, c.terraform_work_dir, c.status, c.created_at, c.updated_at FROM container_instances c LEFT JOIN network_profiles n ON n.id = c.network_profile_id`
	args := []any{}
	if scope.IsCustomer() {
		query += ` WHERE c.customer_account_id = $1`
		args = append(args, scope.CustomerAccountID)
	}
	query += ` ORDER BY c.created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
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

func (s *Service) Get(id int64, scopes ...tenant.Scope) (Instance, error) {
	scope := tenant.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	var rec Instance
	query := `SELECT c.id, COALESCE(c.customer_account_id, 0), c.name, c.description, c.proxmox_credential_id, c.proxmox_node_name, c.network_profile_id, COALESCE(n.name, ''), COALESCE(c.template_id, 0), c.template_path, COALESCE(c.ssh_key_pair_id, 0), c.container_name, c.ip_mode, c.ip_address, c.cpu, c.memory_mb, c.disk_gb, c.unprivileged, c.nesting, c.terraform_work_dir, c.status, c.created_at, c.updated_at FROM container_instances c LEFT JOIN network_profiles n ON n.id = c.network_profile_id WHERE c.id = $1`
	args := []any{id}
	if scope.IsCustomer() {
		query += ` AND c.customer_account_id = $2`
		args = append(args, scope.CustomerAccountID)
	}
	err := s.db.QueryRow(query, args...).Scan(
		&rec.ID, &rec.CustomerAccountID, &rec.Name, &rec.Description, &rec.ProxmoxCredentialID, &rec.ProxmoxNodeName, &rec.NetworkProfileID, &rec.NetworkName, &rec.TemplateID, &rec.TemplatePath, &rec.SSHKeyPairID, &rec.ContainerName, &rec.IPMode, &rec.IPAddress, &rec.CPU, &rec.MemoryMB, &rec.DiskGB, &rec.Unprivileged, &rec.Nesting, &rec.TerraformWorkDir, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, ErrContainerNotFound
	}
	if err != nil {
		return Instance{}, fmt.Errorf("get container: %w", err)
	}
	return rec, nil
}

func (s *Service) Create(req CreateRequest) (CreateResponse, error) {
	req.Name = sanitizeName(req.Name)
	if req.Name == "" {
		return CreateResponse{}, errors.New("container name is required")
	}
	if req.ProxmoxCredentialID <= 0 || strings.TrimSpace(req.ProxmoxNodeName) == "" || req.NetworkProfileID <= 0 {
		return CreateResponse{}, errors.New("proxmox credential, node, and network are required")
	}
	if req.CPU <= 0 {
		req.CPU = 1
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 512
	}
	if req.DiskGB <= 0 {
		req.DiskGB = 8
	}
	if req.IPMode == "" {
		req.IPMode = IPModeDHCP
	}
	if req.IPMode != IPModeDHCP && req.IPMode != IPModeManual && req.IPMode != IPModeAuto {
		return CreateResponse{}, errors.New("ip_mode must be dhcp, manual, or auto")
	}
	if req.IPMode == IPModeManual && strings.TrimSpace(req.IPAddress) == "" {
		return CreateResponse{}, errors.New("ip_address is required for manual IP mode")
	}
	osType := templateOSType(req.TemplateID, req.TemplatePath)
	if req.TemplateID > 0 {
		tpl, err := s.GetTemplate(req.TemplateID)
		if err != nil {
			return CreateResponse{}, err
		}
		if strings.TrimSpace(req.TemplatePath) == "" {
			req.TemplatePath = tpl.TemplatePath
		}
		osType = strings.TrimSpace(tpl.OSType)
	}
	if osType == "" {
		osType = "debian"
	}
	if strings.TrimSpace(req.TemplatePath) == "" {
		return CreateResponse{}, errors.New("container template path is required")
	}
	if strings.TrimSpace(req.RootPassword) == "" {
		return CreateResponse{}, errors.New("root password is required")
	}

	network, err := s.networks.GetByID(req.NetworkProfileID)
	if err != nil {
		return CreateResponse{}, fmt.Errorf("load network profile: %w", err)
	}
	if req.IPMode == IPModeDHCP {
		req.IPAddress = ""
	}
	if req.IPMode == IPModeManual {
		req.IPAddress, err = validateIPAddress(req.IPAddress, network)
		if err != nil {
			return CreateResponse{}, err
		}
	}
	if req.IPMode == IPModeAuto {
		req.IPAddress, err = s.allocateIP(network)
		if err != nil {
			return CreateResponse{}, err
		}
	}

	sshPublicKey := ""
	if req.SSHKeyPairID > 0 {
		key, err := s.sshKeys.GetByID(req.SSHKeyPairID)
		if err != nil {
			return CreateResponse{}, fmt.Errorf("load ssh key: %w", err)
		}
		sshPublicKey = key.PublicKey
	}

	rootRef, err := s.secrets.Put(context.Background(), "containers/"+req.Name, "root-password", []byte(strings.TrimSpace(req.RootPassword)))
	if err != nil {
		return CreateResponse{}, fmt.Errorf("store container root password: %w", err)
	}
	workDir := s.terraformRunner.WorkspaceDirForKey("container-" + req.Name)
	var id int64
	err = s.db.QueryRow(
		`INSERT INTO container_instances (name, customer_account_id, description, proxmox_credential_id, proxmox_node_name, network_profile_id, template_id, template_path, ssh_key_pair_id, container_name, ip_mode, ip_address, cpu, memory_mb, disk_gb, unprivileged, nesting, root_password_secret_ref, terraform_work_dir, status)
		 VALUES ($1,NULLIF($2,0),$3,$4,$5,$6,NULLIF($7,0),$8,NULLIF($9,0),$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,'created')
		 RETURNING id`,
		req.Name, req.CustomerAccountID, strings.TrimSpace(req.Description), req.ProxmoxCredentialID, strings.TrimSpace(req.ProxmoxNodeName), req.NetworkProfileID, req.TemplateID, strings.TrimSpace(req.TemplatePath), req.SSHKeyPairID, req.Name, req.IPMode, strings.TrimSpace(req.IPAddress), req.CPU, req.MemoryMB, req.DiskGB, req.Unprivileged, req.Nesting, rootRef, workDir,
	).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return CreateResponse{}, ErrContainerExists
		}
		return CreateResponse{}, fmt.Errorf("insert container: %w", err)
	}

	spec := terraform.StandaloneContainerSpec{
		WorkspaceKey:    "container-" + req.Name,
		Name:            req.Name,
		ProxmoxNodeName: strings.TrimSpace(req.ProxmoxNodeName),
		CPU:             req.CPU,
		MemoryMB:        req.MemoryMB,
		DiskGB:          req.DiskGB,
		NetworkCIDR:     network.CIDR,
		Gateway:         network.Gateway,
		DNS:             network.DNS,
		Bridge:          network.Bridge,
		VLAN:            network.VLAN,
		TemplateImage:   strings.TrimSpace(req.TemplatePath),
		OSType:          osType,
		SSHPublicKey:    sshPublicKey,
		RootPassword:    strings.TrimSpace(req.RootPassword),
		IPAddress:       strings.TrimSpace(req.IPAddress),
		Unprivileged:    req.Unprivileged,
		Nesting:         req.Nesting,
	}
	workDir, err = s.terraformRunner.PrepareStandaloneContainerWorkspace(spec)
	if err != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-prepare", "error", "Terraform workspace generation failed", err.Error())
		return CreateResponse{}, err
	}
	_, _ = s.db.Exec(`UPDATE container_instances SET terraform_work_dir = $2, updated_at = NOW() WHERE id = $1`, id, workDir)
	_ = s.insertEvent(id, "terraform-prepare", "success", "Terraform workspace generated", workDir)

	resp := CreateResponse{ContainerID: id, Status: "created", TerraformWorkDir: workDir, Warnings: []string{}}
	if req.Apply || s.autoApply {
		action, err := s.Deploy(id)
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
	_ = s.insertEvent(id, "terraform-apply", "info", "Starting Terraform apply for container", rec.TerraformWorkDir)
	result, err := s.terraformRunner.ApplyStandaloneContainer(rec.TerraformWorkDir, terraformAuthFromCredential(cred))
	if err != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-apply", "error", "Terraform apply failed", result.Output+"\n"+err.Error())
		return ActionResponse{ContainerID: id, Action: "deploy", Status: "failed", Output: result.Output, Warnings: []string{}}, err
	}
	_ = s.updateStatus(id, "running")
	_ = s.insertEvent(id, "terraform-apply", "success", "Container deployed successfully", result.Output)
	return ActionResponse{ContainerID: id, Action: "deploy", Status: "running", Output: result.Output, Warnings: []string{}}, nil
}

func (s *Service) Redeploy(id int64) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	if strings.TrimSpace(rec.TerraformWorkDir) == "" {
		return ActionResponse{}, errors.New("terraform workspace is not configured for this container")
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	auth := terraformAuthFromCredential(cred)
	_ = s.updateStatus(id, "redeploying")
	_ = s.insertEvent(id, "redeploy", "info", "Starting container reinstall", rec.TerraformWorkDir)
	destroyResult, destroyErr := s.terraformRunner.DestroyStandaloneContainer(rec.TerraformWorkDir, auth)
	if destroyErr != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-destroy", "error", "Terraform destroy failed during container redeploy", destroyResult.Output+"\n"+destroyErr.Error())
		return ActionResponse{ContainerID: id, Action: "redeploy", Status: "failed", Output: destroyResult.Output, Warnings: []string{}}, destroyErr
	}
	_ = s.insertEvent(id, "terraform-destroy", "success", "Existing container infrastructure destroyed for redeploy", destroyResult.Output)
	applyResult, applyErr := s.terraformRunner.ApplyStandaloneContainer(rec.TerraformWorkDir, auth)
	output := strings.TrimSpace(destroyResult.Output + "\n" + applyResult.Output)
	if applyErr != nil {
		_ = s.updateStatus(id, "failed")
		_ = s.insertEvent(id, "terraform-apply", "error", "Terraform apply failed during container redeploy", applyResult.Output+"\n"+applyErr.Error())
		return ActionResponse{ContainerID: id, Action: "redeploy", Status: "failed", Output: output, Warnings: []string{}}, applyErr
	}
	_ = s.updateStatus(id, "running")
	_ = s.insertEvent(id, "terraform-apply", "success", "Container redeployed successfully", applyResult.Output)
	return ActionResponse{ContainerID: id, Action: "redeploy", Status: "running", Output: output, Warnings: []string{}}, nil
}

func (s *Service) PowerAction(id int64, action string) (ActionResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ActionResponse{}, err
	}
	action = strings.TrimSpace(strings.ToLower(action))
	if action != "start" && action != "restart" && action != "shutdown" {
		return ActionResponse{}, fmt.Errorf("unsupported container action %q", action)
	}
	proxmoxAction := action
	if action == "restart" {
		proxmoxAction = "reboot"
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ActionResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	actual, err := s.proxmoxCredentials.ControlContainerByName(cred, rec.ProxmoxNodeName, rec.ContainerName, proxmoxAction)
	if err != nil {
		_ = s.insertEvent(id, action, "error", "Proxmox container power action failed", err.Error())
		return ActionResponse{ContainerID: id, Action: action, Status: rec.Status, Warnings: []string{err.Error()}}, err
	}
	newStatus := "running"
	if action == "shutdown" {
		newStatus = "stopped"
	}
	_ = s.updateStatus(id, newStatus)
	_ = s.insertEvent(id, action, "success", "Proxmox container power action completed", actual)
	return ActionResponse{ContainerID: id, Action: action, Status: newStatus, Output: actual, Warnings: []string{}}, nil
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
	status, err := s.proxmoxCredentials.GetContainerStatusByName(cred, rec.ProxmoxNodeName, rec.ContainerName)
	if err != nil {
		return ActionResponse{}, err
	}
	mapped := strings.TrimSpace(strings.ToLower(status))
	_ = s.updateStatus(id, mapped)
	return ActionResponse{ContainerID: id, Action: "refresh-status", Status: mapped, Output: status, Warnings: []string{}}, nil
}

func (s *Service) Delete(id int64, confirmationName string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmationName) != rec.Name {
		return errors.New("container name confirmation does not match")
	}
	if strings.TrimSpace(rec.TerraformWorkDir) != "" {
		cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
		if err != nil {
			return fmt.Errorf("load proxmox credentials: %w", err)
		}
		result, destroyErr := s.terraformRunner.DestroyStandaloneContainer(rec.TerraformWorkDir, terraformAuthFromCredential(cred))
		if destroyErr != nil {
			_ = s.insertEvent(id, "terraform-destroy", "error", "Terraform destroy failed", result.Output+"\n"+destroyErr.Error())
			return destroyErr
		}
		_ = s.insertEvent(id, "terraform-destroy", "success", "Container infrastructure destroyed", result.Output)
	}
	if _, err := s.db.Exec(`DELETE FROM container_instances WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete container: %w", err)
	}
	if strings.TrimSpace(rec.TerraformWorkDir) != "" {
		_ = os.RemoveAll(rec.TerraformWorkDir)
	}
	return nil
}

func (s *Service) ListEvents(id int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, container_id, action, level, message, output, created_at FROM container_events WHERE container_id = $1 ORDER BY created_at DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list container events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.ContainerID, &event.Action, &event.Level, &event.Message, &event.Output, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (s *Service) ListTemplates() ([]Template, error) {
	rows, err := s.db.Query(`SELECT id, name, template_path, os_type, description, is_default, created_at, updated_at FROM container_templates ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list container templates: %w", err)
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var tpl Template
		if err := rows.Scan(&tpl.ID, &tpl.Name, &tpl.TemplatePath, &tpl.OSType, &tpl.Description, &tpl.IsDefault, &tpl.CreatedAt, &tpl.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, nil
}

func (s *Service) CreateTemplate(req CreateTemplateRequest) (Template, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.TemplatePath = strings.TrimSpace(req.TemplatePath)
	req.OSType = strings.TrimSpace(req.OSType)
	if req.Name == "" || req.TemplatePath == "" {
		return Template{}, errors.New("template name and path are required")
	}
	if req.OSType == "" {
		req.OSType = "debian"
	}
	if req.IsDefault {
		if _, err := s.db.Exec(`UPDATE container_templates SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return Template{}, err
		}
	}
	var tpl Template
	err := s.db.QueryRow(
		`INSERT INTO container_templates (name, template_path, os_type, description, is_default)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (template_path) DO UPDATE SET name = EXCLUDED.name, os_type = EXCLUDED.os_type, description = EXCLUDED.description, is_default = EXCLUDED.is_default, updated_at = NOW()
		 RETURNING id, name, template_path, os_type, description, is_default, created_at, updated_at`,
		req.Name, req.TemplatePath, req.OSType, strings.TrimSpace(req.Description), req.IsDefault,
	).Scan(&tpl.ID, &tpl.Name, &tpl.TemplatePath, &tpl.OSType, &tpl.Description, &tpl.IsDefault, &tpl.CreatedAt, &tpl.UpdatedAt)
	return tpl, err
}

func (s *Service) GetTemplate(id int64) (Template, error) {
	var tpl Template
	err := s.db.QueryRow(`SELECT id, name, template_path, os_type, description, is_default, created_at, updated_at FROM container_templates WHERE id = $1`, id).
		Scan(&tpl.ID, &tpl.Name, &tpl.TemplatePath, &tpl.OSType, &tpl.Description, &tpl.IsDefault, &tpl.CreatedAt, &tpl.UpdatedAt)
	if err != nil {
		return Template{}, err
	}
	return tpl, nil
}

func (s *Service) allocateIP(profile networks.Profile) (string, error) {
	_, cidrNet, err := net.ParseCIDR(profile.CIDR)
	if err != nil {
		return "", errors.New("network profile cidr is invalid")
	}
	used := map[string]struct{}{}
	used[strings.TrimSpace(profile.Gateway)] = struct{}{}
	reserved, err := s.networks.GetReservedIPsByNetworkProfileID(profile.ID)
	if err != nil {
		return "", fmt.Errorf("load reserved IPs: %w", err)
	}
	for _, r := range reserved {
		used[strings.TrimSpace(r.IPAddress)] = struct{}{}
	}
	rows, err := s.db.Query(`SELECT ip_address FROM container_instances WHERE network_profile_id = $1 AND ip_address <> ''`, profile.ID)
	if err != nil {
		return "", fmt.Errorf("query container ips: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err == nil {
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
	_, err := s.db.Exec(`UPDATE container_instances SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	return err
}

func (s *Service) insertEvent(id int64, action, level, message, output string) error {
	_, err := s.db.Exec(`INSERT INTO container_events (container_id, action, level, message, output) VALUES ($1, $2, $3, $4, $5)`, id, action, level, message, output)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInstance(row rowScanner, rec *Instance) error {
	return row.Scan(&rec.ID, &rec.CustomerAccountID, &rec.Name, &rec.Description, &rec.ProxmoxCredentialID, &rec.ProxmoxNodeName, &rec.NetworkProfileID, &rec.NetworkName, &rec.TemplateID, &rec.TemplatePath, &rec.SSHKeyPairID, &rec.ContainerName, &rec.IPMode, &rec.IPAddress, &rec.CPU, &rec.MemoryMB, &rec.DiskGB, &rec.Unprivileged, &rec.Nesting, &rec.TerraformWorkDir, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt)
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	replacer := strings.NewReplacer(" ", "-", "_", "-", ".", "-")
	return replacer.Replace(name)
}

func terraformAuthFromCredential(cred proxmox.Credential) terraform.ProxmoxAuth {
	return terraform.ProxmoxAuth{
		Endpoint:       cred.Endpoint,
		Username:       cred.Username,
		Password:       cred.Password,
		APITokenID:     cred.APITokenID,
		APITokenSecret: cred.APITokenSecret,
		InsecureTLS:    cred.InsecureTLS,
	}
}

func templateOSType(templateID int64, templatePath string) string {
	lower := strings.ToLower(templatePath)
	switch {
	case strings.Contains(lower, "ubuntu"):
		return "ubuntu"
	case strings.Contains(lower, "alpine"):
		return "alpine"
	case strings.Contains(lower, "centos"):
		return "centos"
	case strings.Contains(lower, "fedora"):
		return "fedora"
	default:
		if templateID > 0 {
			return "debian"
		}
		return "debian"
	}
}
