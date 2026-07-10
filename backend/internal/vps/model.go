package vps

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"proxacloud/backend/internal/networks"
	"proxacloud/backend/internal/proxmox"
	"proxacloud/backend/internal/secrets"
	"proxacloud/backend/internal/sshkeys"
	"proxacloud/backend/internal/tenant"
	"proxacloud/backend/internal/terraform"

	"golang.org/x/crypto/ssh"
)

const (
	IPModeDHCP                   = "dhcp"
	IPModeManual                 = "manual"
	IPModeAuto                   = "auto"
	defaultCloudImageStorageID   = "cloud-images"
	defaultCloudImageContentType = "import"
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

type RegionStore interface {
	PickNode(regionID int64) (int64, string, error)
	NetworkAllowed(regionID int64, networkProfileID int64) (bool, error)
}

type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	ControlVMByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, action string) (string, error)
	GetVMStatusByName(cred proxmox.Credential, proxmoxNodeName string, vmName string) (string, error)
	RunVMGuestCommandByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, command []string) (string, string, error)
	SetVMHostnameByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, hostname string) error
	SetVMUserPasswordByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, username string, password string) error
	VMConsoleURLByName(cred proxmox.Credential, proxmoxNodeName string, vmName string) (proxmox.VMConsole, error)
	CreateVMConsoleSessionByName(cred proxmox.Credential, proxmoxNodeName string, vmName string) (proxmox.VMConsoleSession, error)
	ProxyVMConsoleSession(cred proxmox.Credential, session proxmox.VMConsoleSession, w http.ResponseWriter, r *http.Request) error
	ProxyVMConsoleByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, w http.ResponseWriter, r *http.Request) error
	ListStorageContent(cred proxmox.Credential, proxmoxNodeName string, storageID string, content string) ([]proxmox.StorageContent, error)
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
	regions            RegionStore
	autoApply          bool
	tunnelConfig       TunnelConfig
	tunnelClient       tunnelAPI
	mailer             tunnelMailer
	consoleSessionsMu  sync.Mutex
	consoleSessions    map[string]consoleSessionRecord
}

type consoleSessionRecord struct {
	VPSID      int64
	Credential proxmox.Credential
	Session    proxmox.VMConsoleSession
	ExpiresAt  time.Time
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
	CustomerAccountID   int64     `json:"customer_account_id,omitempty"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	ProxmoxCredentialID int64     `json:"proxmox_credential_id"`
	ProxmoxNodeName     string    `json:"proxmox_node_name"`
	RegionID            int64     `json:"region_id,omitempty"`
	RegionName          string    `json:"region_name,omitempty"`
	NetworkProfileID    int64     `json:"network_profile_id"`
	NetworkName         string    `json:"network_name,omitempty"`
	CloudInitImageID    int64     `json:"cloud_init_image_id"`
	CloudInitImagePath  string    `json:"cloud_init_image_path"`
	SSHKeyPairID        int64     `json:"ssh_key_pair_id"`
	VMName              string    `json:"vm_name"`
	Hostname            string    `json:"hostname"`
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
	RegionID            int64  `json:"region_id"`
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
	CustomerAccountID   int64  `json:"-"`
}

type UpdateRequest struct {
	Name string `json:"name"`
}

type UpdateHostnameRequest struct {
	Hostname string `json:"hostname"`
}

type CreateResponse struct {
	VPSID            int64             `json:"vps_id"`
	Status           string            `json:"status"`
	TerraformWorkDir string            `json:"terraform_work_dir"`
	TerraformOutput  string            `json:"terraform_output,omitempty"`
	Warnings         []string          `json:"warnings"`
	Tunnel           *TunnelAttachment `json:"tunnel,omitempty"`
}

type LoginDetails struct {
	VPSID           int64  `json:"vps_id"`
	Hostname        string `json:"hostname"`
	IPAddress       string `json:"ip_address"`
	SSHUser         string `json:"ssh_user"`
	RootPassword    string `json:"root_password,omitempty"`
	HasRootPassword bool   `json:"has_root_password"`
}

type ResetRootPasswordRequest struct {
	Password string `json:"password"`
}

type ResetRootPasswordResponse struct {
	VPSID        int64  `json:"vps_id"`
	RootPassword string `json:"root_password"`
	Output       string `json:"output,omitempty"`
}

type ActionResponse struct {
	VPSID    int64    `json:"vps_id"`
	Action   string   `json:"action"`
	Status   string   `json:"status"`
	Output   string   `json:"output,omitempty"`
	Warnings []string `json:"warnings"`
}

type ConsoleResponse struct {
	VPSID         int64    `json:"vps_id"`
	VMID          int      `json:"vmid"`
	NodeName      string   `json:"node_name"`
	URL           string   `json:"url"`
	SessionID     string   `json:"session_id"`
	WebSocketPath string   `json:"websocket_path"`
	Password      string   `json:"password"`
	Warnings      []string `json:"warnings"`
}

func NewService(db *sql.DB, networkStore NetworkStore, nodeInventory NodeInventory, proxmoxCredentials ProxmoxCredentialStore, sshKeys SSHKeyPairStore, secretStore secrets.Store, terraformRunner *terraform.Runner, regionStore RegionStore, autoApply bool) *Service {
	return &Service{
		db:                 db,
		networks:           networkStore,
		nodes:              nodeInventory,
		proxmoxCredentials: proxmoxCredentials,
		sshKeys:            sshKeys,
		secrets:            secretStore,
		terraformRunner:    terraformRunner,
		regions:            regionStore,
		autoApply:          autoApply,
		consoleSessions:    make(map[string]consoleSessionRecord),
	}
}

func (s *Service) List(scope tenant.Scope) ([]Instance, error) {
	query := `SELECT v.id, COALESCE(v.customer_account_id, 0), v.name, v.description, v.proxmox_credential_id, v.proxmox_node_name, COALESCE(v.region_id, 0), COALESCE(r.name, ''), v.network_profile_id, COALESCE(n.name, ''), COALESCE(v.cloud_init_image_id, 0), v.cloud_init_image_path, COALESCE(v.ssh_key_pair_id, 0), v.vm_name, COALESCE(NULLIF(v.hostname, ''), v.vm_name), v.ip_mode, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.ssh_user, v.terraform_work_dir, v.status, v.created_at, v.updated_at FROM vps_instances v LEFT JOIN regions r ON r.id = v.region_id LEFT JOIN network_profiles n ON n.id = v.network_profile_id`
	args := []any{}
	if scope.IsCustomer() {
		query += ` WHERE v.customer_account_id = $1`
		args = append(args, scope.CustomerAccountID)
	}
	query += ` ORDER BY v.created_at DESC`
	rows, err := s.db.Query(query, args...)
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

func (s *Service) Get(id int64, scopes ...tenant.Scope) (Instance, error) {
	scope := tenant.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	var rec Instance
	query := `SELECT v.id, COALESCE(v.customer_account_id, 0), v.name, v.description, v.proxmox_credential_id, v.proxmox_node_name, COALESCE(v.region_id, 0), COALESCE(r.name, ''), v.network_profile_id, COALESCE(n.name, ''), COALESCE(v.cloud_init_image_id, 0), v.cloud_init_image_path, COALESCE(v.ssh_key_pair_id, 0), v.vm_name, COALESCE(NULLIF(v.hostname, ''), v.vm_name), v.ip_mode, v.ip_address, v.cpu, v.memory_mb, v.disk_gb, v.ssh_user, v.terraform_work_dir, v.status, v.created_at, v.updated_at FROM vps_instances v LEFT JOIN regions r ON r.id = v.region_id LEFT JOIN network_profiles n ON n.id = v.network_profile_id WHERE v.id = $1`
	args := []any{id}
	if scope.IsCustomer() {
		query += ` AND v.customer_account_id = $2`
		args = append(args, scope.CustomerAccountID)
	}
	err := s.db.QueryRow(query, args...).Scan(
		&rec.ID,
		&rec.CustomerAccountID,
		&rec.Name,
		&rec.Description,
		&rec.ProxmoxCredentialID,
		&rec.ProxmoxNodeName,
		&rec.RegionID,
		&rec.RegionName,
		&rec.NetworkProfileID,
		&rec.NetworkName,
		&rec.CloudInitImageID,
		&rec.CloudInitImagePath,
		&rec.SSHKeyPairID,
		&rec.VMName,
		&rec.Hostname,
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

func (s *Service) Update(id int64, req UpdateRequest, scopes ...tenant.Scope) (Instance, error) {
	name := sanitizeName(req.Name)
	if name == "" {
		return Instance{}, errors.New("name is required")
	}
	scope := tenant.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	query := `UPDATE vps_instances SET name = $2, updated_at = NOW() WHERE id = $1`
	args := []any{id, name}
	if scope.IsCustomer() {
		query += ` AND customer_account_id = $3`
		args = append(args, scope.CustomerAccountID)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Instance{}, ErrVPSExists
		}
		return Instance{}, fmt.Errorf("update vps: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Instance{}, fmt.Errorf("update vps: %w", err)
	}
	if affected == 0 {
		return Instance{}, ErrVPSNotFound
	}
	return s.Get(id, scope)
}

func (s *Service) UpdateHostname(id int64, req UpdateHostnameRequest, scopes ...tenant.Scope) (LoginDetails, error) {
	hostname := sanitizeHostname(req.Hostname)
	if hostname == "" {
		return LoginDetails{}, errors.New("hostname is required")
	}
	scope := tenant.Scope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	rec, err := s.Get(id, scope)
	if err != nil {
		return LoginDetails{}, err
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return LoginDetails{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	outputParts := []string{}
	if err := s.proxmoxCredentials.SetVMHostnameByName(cred, rec.ProxmoxNodeName, rec.VMName, hostname); err != nil {
		if !isUnsupportedNativeHostnameAPI(err) {
			_ = s.insertEvent(id, "hostname-update", "error", "Hostname update failed", err.Error())
			return LoginDetails{}, err
		}
		stdout, stderr, guestExecErr := s.proxmoxCredentials.RunVMGuestCommandByName(cred, rec.ProxmoxNodeName, rec.VMName, hostnameGuestExecCommand(hostname))
		if guestExecErr != nil {
			sshOutput, sshErr := s.updateHostnameBySSH(rec, hostname)
			if sshErr != nil {
				message := fmt.Sprintf("Proxmox guest hostname API is not available on this node, guest-exec failed: %v, and SSH fallback failed: %v", guestExecErr, sshErr)
				_ = s.insertEvent(id, "hostname-update", "error", "Hostname update failed", message)
				return LoginDetails{}, errors.New(message)
			}
			outputParts = append(outputParts, "Guest hostname updated through SSH fallback.")
			outputParts = append(outputParts, strings.TrimSpace(sshOutput))
		} else {
			outputParts = append(outputParts, "Guest hostname updated through QEMU guest-exec fallback.")
			outputParts = append(outputParts, strings.TrimSpace(strings.Join([]string{stdout, stderr}, "\n")))
		}
	} else {
		outputParts = append(outputParts, "Guest hostname updated through Proxmox QEMU guest agent.")
	}
	stdout, stderr, persistenceErr := s.proxmoxCredentials.RunVMGuestCommandByName(cred, rec.ProxmoxNodeName, rec.VMName, hostnamePersistenceCommand(hostname))
	if persistenceErr != nil {
		message := "Could not update cloud-init hostname persistence because guest-exec is unavailable: " + persistenceErr.Error()
		outputParts = append(outputParts, message)
		_ = s.insertEvent(id, "hostname-update", "warning", "Hostname updated, cloud-init persistence was not changed", persistenceErr.Error())
	} else {
		outputParts = append(outputParts, strings.TrimSpace(strings.Join([]string{stdout, stderr}, "\n")))
	}
	output := strings.TrimSpace(strings.Join(outputParts, "\n"))
	query := `UPDATE vps_instances SET hostname = $2, updated_at = NOW() WHERE id = $1`
	args := []any{id, hostname}
	if scope.IsCustomer() {
		query += ` AND customer_account_id = $3`
		args = append(args, scope.CustomerAccountID)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return LoginDetails{}, fmt.Errorf("update vps hostname: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return LoginDetails{}, fmt.Errorf("update vps hostname: %w", err)
	}
	if affected == 0 {
		return LoginDetails{}, ErrVPSNotFound
	}
	_ = s.insertEvent(id, "hostname-update", "success", "Hostname updated", output)
	return s.LoginDetails(id)
}

func (s *Service) LoginDetails(id int64) (LoginDetails, error) {
	rec, err := s.Get(id)
	if err != nil {
		return LoginDetails{}, err
	}

	details := LoginDetails{
		VPSID:     rec.ID,
		Hostname:  rec.Hostname,
		IPAddress: rec.IPAddress,
		SSHUser:   rec.SSHUser,
	}
	if details.Hostname == "" {
		details.Hostname = rec.VMName
	}

	ref, err := s.rootPasswordSecretRef(id)
	if err != nil {
		return LoginDetails{}, err
	}
	if strings.TrimSpace(ref) != "" {
		details.HasRootPassword = true
		if s.secrets != nil {
			value, err := s.secrets.Get(context.Background(), ref)
			if err != nil {
				if !errors.Is(err, secrets.ErrSecretNotFound) {
					return LoginDetails{}, err
				}
			} else {
				details.RootPassword = string(value)
			}
		}
	}
	return details, nil
}

func isUnsupportedNativeHostnameAPI(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "status 501") ||
		strings.Contains(message, "not implemented") ||
		strings.Contains(message, "set-host-name")
}

func (s *Service) updateHostnameBySSH(rec Instance, hostname string) (string, error) {
	if strings.TrimSpace(rec.IPAddress) == "" {
		return "", errors.New("vps ip address is not available")
	}
	ref, err := s.rootPasswordSecretRef(rec.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("root password is not stored for this vps")
	}
	if s.secrets == nil {
		return "", errors.New("secret store is not configured")
	}
	value, err := s.secrets.Get(context.Background(), ref)
	if err != nil {
		return "", fmt.Errorf("load root password secret: %w", err)
	}
	password := strings.TrimSpace(string(value))
	if password == "" {
		return "", errors.New("stored root password is empty")
	}

	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}
	address := net.JoinHostPort(strings.TrimSpace(rec.IPAddress), "22")
	client, err := ssh.Dial("tcp", address, clientConfig)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", address, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("create ssh session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("bash -lc " + shellQuote(hostnameUpdateScript(hostname)))
	if err != nil {
		return string(output), fmt.Errorf("run hostname command over ssh: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (s *Service) ResetRootPassword(id int64, req ResetRootPasswordRequest) (ResetRootPasswordResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ResetRootPasswordResponse{}, err
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		password, err = generateRootPassword(16)
		if err != nil {
			return ResetRootPasswordResponse{}, err
		}
	}
	if err := validateRootPassword(password); err != nil {
		return ResetRootPasswordResponse{}, err
	}
	if s.secrets == nil {
		return ResetRootPasswordResponse{}, errors.New("secret store is not configured")
	}

	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ResetRootPasswordResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	var passwordErr error
	for attempt := 0; attempt < 6; attempt++ {
		passwordErr = s.proxmoxCredentials.SetVMUserPasswordByName(cred, rec.ProxmoxNodeName, rec.VMName, "root", password)
		if passwordErr == nil {
			break
		}
		if !isQEMUAgentUnavailable(passwordErr) || attempt == 5 {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if passwordErr != nil {
		_ = s.insertEvent(id, "root-password-reset", "error", "Root password reset failed", passwordErr.Error())
		if isQEMUAgentUnavailable(passwordErr) {
			return ResetRootPasswordResponse{}, errors.New("QEMU guest agent is not running inside the VM. Start qemu-guest-agent from the VM console, wait for cloud-init to finish after boot, then retry")
		}
		return ResetRootPasswordResponse{}, passwordErr
	}

	outputParts := []string{"Root password updated through Proxmox QEMU guest agent."}
	stdout, stderr, guestExecErr := s.proxmoxCredentials.RunVMGuestCommandByName(cred, rec.ProxmoxNodeName, rec.VMName, rootPasswordSSHEnableCommand())
	if guestExecErr != nil {
		message := "Could not update SSH password-login settings because guest-exec is unavailable: " + guestExecErr.Error()
		outputParts = append(outputParts, message)
		_ = s.insertEvent(id, "root-password-reset", "warning", "Root password reset succeeded, SSH login settings were not changed", guestExecErr.Error())
	} else {
		output := strings.TrimSpace(stdout)
		if trimmedErr := strings.TrimSpace(stderr); trimmedErr != "" {
			if output != "" {
				output += "\n"
			}
			output += trimmedErr
		}
		if output != "" {
			outputParts = append(outputParts, output)
		}
	}

	oldRef, err := s.rootPasswordSecretRef(id)
	if err != nil {
		return ResetRootPasswordResponse{}, err
	}
	newRef, err := s.secrets.Put(context.Background(), "vps/"+sanitizeName(rec.Name), "root-password", []byte(password))
	if err != nil {
		return ResetRootPasswordResponse{}, fmt.Errorf("store root password: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE vps_instances SET root_password_secret_ref = $2, updated_at = NOW() WHERE id = $1`, id, newRef); err != nil {
		_ = s.secrets.Delete(context.Background(), newRef)
		return ResetRootPasswordResponse{}, fmt.Errorf("update root password secret ref: %w", err)
	}
	if strings.TrimSpace(oldRef) != "" && oldRef != newRef {
		_ = s.secrets.Delete(context.Background(), oldRef)
	}

	output := strings.Join(outputParts, "\n")
	_ = s.insertEvent(id, "root-password-reset", "success", "Root password reset", output)
	return ResetRootPasswordResponse{VPSID: id, RootPassword: password, Output: output}, nil
}

func (s *Service) Create(req CreateRequest) (CreateResponse, error) {
	if err := s.validateCreateRequest(&req); err != nil {
		return CreateResponse{}, err
	}

	if req.RegionID > 0 {
		if s.regions == nil {
			return CreateResponse{}, errors.New("regions are not configured")
		}
		credentialID, nodeName, err := s.regions.PickNode(req.RegionID)
		if err != nil {
			return CreateResponse{}, err
		}
		req.ProxmoxCredentialID = credentialID
		req.ProxmoxNodeName = nodeName
		allowed, err := s.regions.NetworkAllowed(req.RegionID, req.NetworkProfileID)
		if err != nil {
			return CreateResponse{}, err
		}
		if !allowed {
			return CreateResponse{}, errors.New("selected network is not available in this location")
		}
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
	warnings := []string{}

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
		`INSERT INTO vps_instances (name, customer_account_id, description, proxmox_credential_id, proxmox_node_name, region_id, network_profile_id, cloud_init_image_id, cloud_init_image_path, ssh_key_pair_id, vm_name, hostname, ip_mode, ip_address, cpu, memory_mb, disk_gb, ssh_user, root_password_secret_ref, terraform_work_dir, status)
		 VALUES ($1, NULLIF($2, 0), $3, $4, $5, NULLIF($6, 0), $7, NULLIF($8, 0), $9, NULLIF($10, 0), $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, 'created')
		 RETURNING id`,
		req.Name,
		req.CustomerAccountID,
		req.Description,
		req.ProxmoxCredentialID,
		req.ProxmoxNodeName,
		req.RegionID,
		req.NetworkProfileID,
		req.CloudInitImageID,
		req.CloudInitImagePath,
		req.SSHKeyPairID,
		req.Name,
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

	agentInstallCommand := ""
	var tunnelAttachment *TunnelAttachment
	if s.tunnelConfig.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		// Use `root` as the SSH user for tunnel access commands
		installCommand, attachment, tunnelErr := s.configureTunnelForCreate(ctx, vpsID, req, "root")
		cancel()
		if tunnelErr != nil {
			warnings = append(warnings, "Proxacloud tunnel setup failed: "+tunnelErr.Error())
			_ = s.insertEvent(vpsID, "tunnel-setup", "error", "Proxacloud tunnel setup failed", tunnelErr.Error())
		} else if attachment != nil {
			agentInstallCommand = installCommand
			tunnelAttachment = attachment
			_ = s.insertEvent(vpsID, "tunnel-setup", "success", "Proxacloud HTTP and TCP tunnel routes configured", attachment.AccessSummary())
		}
	}

	spec := terraform.StandaloneVPSSpec{
		WorkspaceKey:        "vps-" + req.Name,
		Name:                req.Name,
		ProxmoxNodeName:     req.ProxmoxNodeName,
		CPU:                 req.CPU,
		MemoryMB:            req.MemoryMB,
		DiskGB:              req.DiskGB,
		NetworkCIDR:         network.CIDR,
		Gateway:             network.Gateway,
		DNS:                 network.DNS,
		Bridge:              network.Bridge,
		VLAN:                network.VLAN,
		CloudInitImage:      req.CloudInitImagePath,
		SSHPublicKey:        sshPublicKey,
		SSHUser:             sshUser,
		RootPassword:        rootPassword,
		IPAddress:           req.IPAddress,
		AgentInstallCommand: agentInstallCommand,
	}
	workDir, err = s.terraformRunner.PrepareStandaloneVPSWorkspace(spec)
	if err != nil {
		_ = s.updateStatus(vpsID, "failed")
		_ = s.insertEvent(vpsID, "terraform-prepare", "error", "Terraform workspace generation failed", err.Error())
		return CreateResponse{}, err
	}
	_, _ = s.db.Exec(`UPDATE vps_instances SET terraform_work_dir = $2, updated_at = NOW() WHERE id = $1`, vpsID, workDir)
	_ = s.insertEvent(vpsID, "terraform-prepare", "success", "Terraform workspace generated", workDir)

	resp := CreateResponse{VPSID: vpsID, Status: "created", TerraformWorkDir: workDir, Warnings: warnings, Tunnel: tunnelAttachment}
	if req.Apply || s.autoApply {
		action, err := s.Deploy(vpsID)
		resp.Status = action.Status
		resp.TerraformOutput = action.Output
		resp.Warnings = append(resp.Warnings, action.Warnings...)
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
	warnings := []string{}
	if err := s.EmailTunnelDetails(id); err != nil {
		warnings = append(warnings, err.Error())
		_ = s.insertEvent(id, "tunnel-email", "error", "VPS tunnel access email failed", err.Error())
	}
	return ActionResponse{VPSID: id, Action: "deploy", Status: "running", Output: result.Output, Warnings: warnings}, nil
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

func (s *Service) Console(id int64) (ConsoleResponse, error) {
	rec, err := s.Get(id)
	if err != nil {
		return ConsoleResponse{}, err
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(rec.ProxmoxCredentialID)
	if err != nil {
		return ConsoleResponse{}, fmt.Errorf("load proxmox credentials: %w", err)
	}
	session, err := s.proxmoxCredentials.CreateVMConsoleSessionByName(cred, rec.ProxmoxNodeName, rec.VMName)
	if err != nil {
		_ = s.insertEvent(id, "console", "error", "Create Proxmox console session failed", err.Error())
		return ConsoleResponse{VPSID: id, Warnings: []string{err.Error()}}, err
	}
	sessionID, err := newConsoleSessionID()
	if err != nil {
		return ConsoleResponse{}, err
	}
	expiresAt := time.Now().Add(2 * time.Minute)
	s.storeConsoleSession(sessionID, consoleSessionRecord{
		VPSID:      id,
		Credential: cred,
		Session:    session,
		ExpiresAt:  expiresAt,
	})

	_ = s.insertEvent(id, "console", "info", "Project console session created", fmt.Sprintf("node=%s vmid=%d", session.NodeName, session.VMID))
	return ConsoleResponse{
		VPSID:         id,
		VMID:          session.VMID,
		NodeName:      session.NodeName,
		URL:           fmt.Sprintf("/vps/%d/console?session=%s", id, sessionID),
		SessionID:     sessionID,
		WebSocketPath: fmt.Sprintf("/api/v1/vps/%d/console/ws?session=%s", id, sessionID),
		Password:      session.Ticket,
		Warnings:      []string{},
	}, nil
}

func (s *Service) ProxyConsole(w http.ResponseWriter, r *http.Request, id int64) error {
	sessionID := strings.TrimSpace(r.URL.Query().Get("session"))
	if sessionID == "" {
		return errors.New("console session is required")
	}
	rec, ok := s.takeConsoleSession(sessionID, id)
	if !ok {
		return errors.New("console session is missing or expired; reconnect the console")
	}
	if err := s.proxmoxCredentials.ProxyVMConsoleSession(rec.Credential, rec.Session, w, r); err != nil {
		_ = s.insertEvent(id, "console", "error", "Project console websocket failed", err.Error())
		return err
	}
	_ = s.insertEvent(id, "console", "info", "Project console websocket closed", fmt.Sprintf("node=%s vmid=%d", rec.Session.NodeName, rec.Session.VMID))
	return nil
}

func (s *Service) Delete(id int64, confirmationName string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmationName) != rec.Name {
		return errors.New("vps name confirmation does not match")
	}
	attachment, err := s.GetTunnelAttachment(id)
	if err != nil && !errors.Is(err, ErrTunnelAttachmentNotFound) {
		return fmt.Errorf("load vps tunnel attachment: %w", err)
	}
	if attachment.ID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := s.cleanupTunnelAttachment(ctx, attachment); err != nil {
			_ = s.insertEvent(id, "tunnel-cleanup", "error", "Proxacloud tunnel cleanup failed", err.Error())
			return err
		}
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
	if err := validateCloudInitImagePath(req.ImagePath); err != nil {
		return CloudInitImage{}, err
	}
	meta := inferCloudImageMetadata(proxmoxStorageFilename(req.ImagePath))
	if req.OSType == "" || (req.OSType == "ubuntu" && meta.OSType != "generic" && meta.OSType != "ubuntu") {
		req.OSType = meta.OSType
	}
	if req.SSHUser == "" {
		req.SSHUser = meta.SSHUser
	}
	if req.DefaultCPU <= 0 {
		req.DefaultCPU = meta.DefaultCPU
	}
	if req.DefaultMemoryMB <= 0 {
		req.DefaultMemoryMB = meta.DefaultMemoryMB
	}
	if req.DefaultDiskGB <= 0 {
		req.DefaultDiskGB = meta.DefaultDiskGB
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

func (s *Service) DiscoverImages(credentialID int64, nodeName string, storageID ...string) ([]CloudInitImage, error) {
	nodeName = strings.TrimSpace(nodeName)
	selectedStorageID := defaultCloudImageStorageID
	if len(storageID) > 0 && strings.TrimSpace(storageID[0]) != "" {
		selectedStorageID = strings.TrimSpace(storageID[0])
	}
	if credentialID <= 0 {
		return nil, errors.New("proxmox credential is required")
	}
	if nodeName == "" {
		return nil, errors.New("proxmox node name is required")
	}
	if err := s.validateNode(credentialID, nodeName); err != nil {
		return nil, err
	}
	cred, err := s.proxmoxCredentials.GetCredentialWithSecrets(credentialID)
	if err != nil {
		return nil, fmt.Errorf("load proxmox credentials: %w", err)
	}
	content, err := s.proxmoxCredentials.ListStorageContent(cred, nodeName, selectedStorageID, defaultCloudImageContentType)
	if err != nil {
		return nil, fmt.Errorf("list Proxmox %s:%s content on node %s: %w", selectedStorageID, defaultCloudImageContentType, nodeName, err)
	}
	discovered := make([]CloudInitImage, 0)
	for _, item := range content {
		volID := strings.TrimSpace(item.VolID)
		if !isSupportedCloudImageVolID(volID) {
			continue
		}
		name := proxmoxStorageFilename(volID)
		meta := inferCloudImageMetadata(name)
		image, err := s.CreateImage(CreateCloudInitImageRequest{
			Name:             meta.Name,
			ImagePath:        volID,
			OSType:           meta.OSType,
			Description:      fmt.Sprintf("Discovered from Proxmox %s:%s on %s", selectedStorageID, defaultCloudImageContentType, nodeName),
			IsDefault:        false,
			SSHUser:          meta.SSHUser,
			DefaultCPU:       meta.DefaultCPU,
			DefaultMemoryMB:  meta.DefaultMemoryMB,
			DefaultDiskGB:    meta.DefaultDiskGB,
			SourcePath:       volID,
			DetectedNodeName: nodeName,
		})
		if err != nil {
			return nil, err
		}
		discovered = append(discovered, image)
	}
	if len(discovered) == 0 {
		return nil, fmt.Errorf("no cloud image files (.qcow2, .img, .raw) found in Proxmox storage %s:%s on node %s. Upload the image under /mnt/pve/%s/import, then discover again", selectedStorageID, defaultCloudImageContentType, nodeName, selectedStorageID)
	}
	sort.Slice(discovered, func(i, j int) bool {
		return strings.ToLower(discovered[i].Name) < strings.ToLower(discovered[j].Name)
	})
	return discovered, nil
}

func (s *Service) DiscoverImagesForRegion(regionID int64, storageID ...string) ([]CloudInitImage, error) {
	if regionID <= 0 {
		return nil, errors.New("region_id is required")
	}
	if s.regions == nil {
		return nil, errors.New("regions are not configured")
	}
	credentialID, nodeName, err := s.regions.PickNode(regionID)
	if err != nil {
		return nil, err
	}
	return s.DiscoverImages(credentialID, nodeName, storageID...)
}

func (s *Service) validateCreateRequest(req *CreateRequest) error {
	req.Name = sanitizeName(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.ProxmoxNodeName = strings.TrimSpace(req.ProxmoxNodeName)
	req.CloudInitImagePath = strings.TrimSpace(req.CloudInitImagePath)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	req.RootPassword = strings.TrimSpace(req.RootPassword)
	req.IPMode = strings.TrimSpace(strings.ToLower(req.IPMode))
	req.IPAddress = strings.TrimSpace(req.IPAddress)
	if req.IPMode == "" {
		req.IPMode = IPModeDHCP
	}
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.RegionID <= 0 && req.ProxmoxCredentialID <= 0 {
		return errors.New("proxmox_credential_id is required")
	}
	if req.RegionID <= 0 && req.ProxmoxNodeName == "" {
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
	if err := validateRootPassword(req.RootPassword); err != nil {
		return err
	}
	return nil
}

func validateRootPassword(password string) error {
	if password == "" {
		return nil
	}
	if strings.ContainsAny(password, "\r\n") {
		return errors.New("root password cannot contain newlines")
	}
	if strings.Contains(password, ":") {
		return errors.New("root password cannot contain colon")
	}
	if len(password) < 10 {
		return errors.New("root password must be at least 10 characters")
	}
	hasLower := false
	hasUpper := false
	hasNumber := false
	hasSpecial := false
	for _, char := range password {
		switch {
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= '0' && char <= '9':
			hasNumber = true
		default:
			hasSpecial = true
		}
	}
	if !hasLower {
		return errors.New("root password must include a lowercase letter")
	}
	if !hasUpper {
		return errors.New("root password must include an uppercase letter")
	}
	if !hasNumber {
		return errors.New("root password must include a number")
	}
	if !hasSpecial {
		return errors.New("root password must include a special character")
	}
	return nil
}

const generatedRootPasswordSpecials = "!@#$%^&*_-+=?"

func generateRootPassword(length int) (string, error) {
	if length < 10 {
		length = 10
	}
	groups := []string{
		"abcdefghijklmnopqrstuvwxyz",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"0123456789",
		generatedRootPasswordSpecials,
	}
	all := strings.Join(groups, "")
	chars := make([]byte, 0, length)
	for _, group := range groups {
		char, err := randomPasswordChar(group)
		if err != nil {
			return "", err
		}
		chars = append(chars, char)
	}
	for len(chars) < length {
		char, err := randomPasswordChar(all)
		if err != nil {
			return "", err
		}
		chars = append(chars, char)
	}
	for i := len(chars) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", fmt.Errorf("generate root password: %w", err)
		}
		j := int(n.Int64())
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars), nil
}

func randomPasswordChar(chars string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
	if err != nil {
		return 0, fmt.Errorf("generate root password: %w", err)
	}
	return chars[n.Int64()], nil
}

func rootPasswordSSHEnableCommand() []string {
	script := strings.Join([]string{
		"set -e",
		"passwd -u root >/dev/null 2>&1 || true",
		"install -d -m 0755 /etc/ssh/sshd_config.d",
		"printf 'PasswordAuthentication yes\\nPermitRootLogin yes\\nKbdInteractiveAuthentication yes\\n' > /etc/ssh/sshd_config.d/99-proxacloud-password-auth.conf",
		"systemctl restart sshd || systemctl restart ssh || true",
	}, "; ")
	return []string{"bash", "-lc", script}
}

func hostnameGuestExecCommand(hostname string) []string {
	return []string{"bash", "-lc", hostnameUpdateScript(hostname)}
}

func hostnamePersistenceCommand(hostname string) []string {
	script := strings.Join([]string{
		"set -e",
		fmt.Sprintf("printf '%%s\\n' %s > /etc/hostname", shellQuote(hostname)),
		"install -d -m 0755 /etc/cloud/cloud.cfg.d",
		"printf 'preserve_hostname: true\\n' > /etc/cloud/cloud.cfg.d/99-proxacloud-hostname.cfg",
	}, "; ")
	return []string{"bash", "-lc", script}
}

func hostnameUpdateScript(hostname string) string {
	return strings.Join([]string{
		"set -e",
		fmt.Sprintf("hostnamectl set-hostname %s", shellQuote(hostname)),
		fmt.Sprintf("printf '%%s\\n' %s > /etc/hostname", shellQuote(hostname)),
		"install -d -m 0755 /etc/cloud/cloud.cfg.d",
		"printf 'preserve_hostname: true\\n' > /etc/cloud/cloud.cfg.d/99-proxacloud-hostname.cfg",
	}, "; ")
}

func sanitizeHostname(hostname string) string {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	hostname = strings.Trim(hostname, ".")
	if hostname == "" || len(hostname) > 253 {
		return ""
	}
	labels := strings.Split(hostname, ".")
	for i, label := range labels {
		label = sanitizeHostnameLabel(label)
		if label == "" || len(label) > 63 {
			return ""
		}
		labels[i] = label
	}
	return strings.Join(labels, ".")
}

func sanitizeHostnameLabel(label string) string {
	var builder strings.Builder
	previousDash := false
	for _, r := range label {
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

func isQEMUAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "qemu guest agent is not running") ||
		strings.Contains(message, "guest agent is not running")
}

func (s *Service) rootPasswordSecretRef(id int64) (string, error) {
	var ref string
	err := s.db.QueryRow(`SELECT COALESCE(root_password_secret_ref, '') FROM vps_instances WHERE id = $1`, id).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVPSNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load root password secret ref: %w", err)
	}
	return ref, nil
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
		if err := validateCloudInitImagePath(image.ImagePath); err != nil {
			return CloudInitImage{}, err
		}
		return image, nil
	}
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return CloudInitImage{}, errors.New("select a cloud-init image or provide image path")
	}
	if err := validateCloudInitImagePath(imagePath); err != nil {
		return CloudInitImage{}, err
	}
	meta := inferCloudImageMetadata(filepath.Base(imagePath))
	return CloudInitImage{ImagePath: imagePath, SSHUser: meta.SSHUser, DefaultCPU: meta.DefaultCPU, DefaultMemoryMB: meta.DefaultMemoryMB, DefaultDiskGB: meta.DefaultDiskGB}, nil
}

func isSupportedCloudImageVolID(volID string) bool {
	lower := strings.ToLower(strings.TrimSpace(volID))
	if !strings.Contains(lower, ":import/") || strings.HasPrefix(lower, ":") {
		return false
	}
	return hasSupportedCloudImageExtension(lower)
}

func hasSupportedCloudImageExtension(name string) bool {
	switch path.Ext(strings.ToLower(strings.TrimSpace(name))) {
	case ".qcow2", ".img", ".raw":
		return true
	default:
		return false
	}
}

func validateCloudInitImagePath(imagePath string) error {
	trimmed := strings.TrimSpace(imagePath)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, ":iso/") && hasSupportedCloudImageExtension(lower) {
		return fmt.Errorf("cloud image %q is stored as Proxmox ISO content. Move the uncompressed cloud image to the shared import storage and use %s:import/%s", trimmed, defaultCloudImageStorageID, proxmoxStorageFilename(trimmed))
	}
	return nil
}

func proxmoxStorageFilename(volID string) string {
	filename := path.Base(strings.TrimSpace(volID))
	if filename == "." || filename == "/" {
		trimmed := strings.TrimSpace(volID)
		if marker := strings.Index(strings.ToLower(trimmed), ":import/"); marker >= 0 {
			trimmed = trimmed[marker+len(":import/"):]
		}
		return strings.TrimPrefix(trimmed, "local:iso/")
	}
	return filename
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

func newConsoleSessionID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate console session id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *Service) storeConsoleSession(id string, rec consoleSessionRecord) {
	now := time.Now()
	s.consoleSessionsMu.Lock()
	defer s.consoleSessionsMu.Unlock()
	for key, existing := range s.consoleSessions {
		if now.After(existing.ExpiresAt) {
			delete(s.consoleSessions, key)
		}
	}
	s.consoleSessions[id] = rec
}

func (s *Service) takeConsoleSession(id string, vpsID int64) (consoleSessionRecord, bool) {
	now := time.Now()
	s.consoleSessionsMu.Lock()
	defer s.consoleSessionsMu.Unlock()
	rec, ok := s.consoleSessions[id]
	if !ok {
		return consoleSessionRecord{}, false
	}
	delete(s.consoleSessions, id)
	if rec.VPSID != vpsID || now.After(rec.ExpiresAt) {
		return consoleSessionRecord{}, false
	}
	return rec, true
}

func scanInstance(rows interface {
	Scan(dest ...any) error
}, rec *Instance) error {
	return rows.Scan(
		&rec.ID,
		&rec.CustomerAccountID,
		&rec.Name,
		&rec.Description,
		&rec.ProxmoxCredentialID,
		&rec.ProxmoxNodeName,
		&rec.RegionID,
		&rec.RegionName,
		&rec.NetworkProfileID,
		&rec.NetworkName,
		&rec.CloudInitImageID,
		&rec.CloudInitImagePath,
		&rec.SSHKeyPairID,
		&rec.VMName,
		&rec.Hostname,
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
