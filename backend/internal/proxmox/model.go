package proxmox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"proxacloud/backend/internal/secrets"

	"github.com/lib/pq"
)

var (
	ErrCredentialNotFound = errors.New("proxmox credential not found")
	ErrCredentialInUse    = errors.New("proxmox credential is in use by one or more clusters")
)

type Credential struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Endpoint       string    `json:"endpoint"`
	Username       string    `json:"username,omitempty"`
	Password       string    `json:"-"`
	APITokenID     string    `json:"api_token_id,omitempty"`
	APITokenSecret string    `json:"-"`
	InsecureTLS    bool      `json:"insecure_tls"`
	CreatedAt      time.Time `json:"created_at"`
}

type Node struct {
	ID               int64     `json:"id"`
	CredentialID     int64     `json:"credential_id"`
	CredentialName   string    `json:"credential_name,omitempty"`
	NodeName         string    `json:"node_name"`
	Status           string    `json:"status"`
	CPUCount         int       `json:"cpu_count"`
	CPUUsage         float64   `json:"cpu_usage"`
	MemoryTotalBytes int64     `json:"memory_total_bytes"`
	MemoryUsedBytes  int64     `json:"memory_used_bytes"`
	DiskTotalBytes   int64     `json:"disk_total_bytes"`
	DiskUsedBytes    int64     `json:"disk_used_bytes"`
	Enabled          bool      `json:"enabled"`
	LastDiscoveredAt time.Time `json:"last_discovered_at"`
}

type StorageContent struct {
	VolID   string `json:"volid"`
	Content string `json:"content"`
	Format  string `json:"format"`
	Size    int64  `json:"size"`
}

type TaskStatus struct {
	UPID       string `json:"upid"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
}

type NodeStorage struct {
	CredentialID     int64  `json:"credential_id"`
	CredentialName   string `json:"credential_name,omitempty"`
	NodeName         string `json:"node_name"`
	StorageID        string `json:"storage_id"`
	Type             string `json:"type"`
	Content          string `json:"content"`
	Enabled          bool   `json:"enabled"`
	Shared           bool   `json:"shared"`
	Active           bool   `json:"active"`
	TotalBytes       int64  `json:"total_bytes"`
	UsedBytes        int64  `json:"used_bytes"`
	AvailableBytes   int64  `json:"available_bytes"`
	UsagePercent     int    `json:"usage_percent"`
	LastDiscoveredAt string `json:"last_discovered_at,omitempty"`
}

type StorageInventoryError struct {
	CredentialID int64  `json:"credential_id"`
	NodeName     string `json:"node_name"`
	Error        string `json:"error"`
}

type StorageInventory struct {
	Items  []NodeStorage           `json:"items"`
	Errors []StorageInventoryError `json:"errors"`
}

type VMConsole struct {
	VMID     int    `json:"vmid"`
	NodeName string `json:"node_name"`
	URL      string `json:"url"`
}

type VMConsoleSession struct {
	VMID     int    `json:"vmid"`
	NodeName string `json:"node_name"`
	Port     string `json:"port"`
	Ticket   string `json:"-"`
	Cert     string `json:"cert,omitempty"`
}

type FirewallRuleSpec struct {
	Direction       string
	Action          string
	Protocol        string
	SourceCIDR      string
	DestinationCIDR string
	PortStart       *int
	PortEnd         *int
	Priority        int
	Description     string
	Enabled         bool
	ManagedComment  string
}

type Service struct {
	db      *sql.DB
	client  *Client
	secrets secrets.Store
}

func NewService(db *sql.DB, secretStore secrets.Store) *Service {
	return &Service{db: db, client: NewClient(), secrets: secretStore}
}

func (s *Service) CreateCredential(in Credential) (Credential, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.Username = strings.TrimSpace(in.Username)
	in.APITokenID = strings.TrimSpace(in.APITokenID)

	if in.Name == "" {
		return Credential{}, errors.New("name is required")
	}
	if in.Endpoint == "" {
		return Credential{}, errors.New("endpoint is required")
	}

	usesUserPass := in.Username != "" && in.Password != ""
	usesToken := in.APITokenID != "" && in.APITokenSecret != ""
	if !usesUserPass && !usesToken {
		return Credential{}, errors.New("provide either username/password or api_token_id/api_token_secret")
	}
	if usesUserPass && !strings.Contains(in.Username, "@") {
		return Credential{}, errors.New("username must include the Proxmox realm, for example user@pve or root@pam")
	}

	passwordRef, tokenSecretRef, err := s.storeCredentialSecrets(context.Background(), 0, in)
	if err != nil {
		return Credential{}, err
	}

	var out Credential
	err = s.db.QueryRow(
		`INSERT INTO proxmox_credentials
		 (name, endpoint, username, password, password_secret_ref, api_token_id, api_token_secret, api_token_secret_ref, insecure_tls)
		 VALUES ($1, $2, NULLIF($3, ''), NULL, NULLIF($4, ''), NULLIF($5, ''), NULL, NULLIF($6, ''), $7)
		 RETURNING id, name, endpoint, COALESCE(username, ''), COALESCE(api_token_id, ''), insecure_tls, created_at`,
		in.Name,
		in.Endpoint,
		in.Username,
		passwordRef,
		in.APITokenID,
		tokenSecretRef,
		in.InsecureTLS,
	).Scan(&out.ID, &out.Name, &out.Endpoint, &out.Username, &out.APITokenID, &out.InsecureTLS, &out.CreatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("insert proxmox credential: %w", err)
	}

	return out, nil
}

func (s *Service) UpdateCredential(id int64, in Credential) (Credential, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.Username = strings.TrimSpace(in.Username)
	in.APITokenID = strings.TrimSpace(in.APITokenID)

	if in.Name == "" {
		return Credential{}, errors.New("name is required")
	}
	if in.Endpoint == "" {
		return Credential{}, errors.New("endpoint is required")
	}

	usesUserPass := in.Username != "" && in.Password != ""
	usesToken := in.APITokenID != "" && in.APITokenSecret != ""
	if !usesUserPass && !usesToken {
		return Credential{}, errors.New("provide either username/password or api_token_id/api_token_secret")
	}
	if usesUserPass && !strings.Contains(in.Username, "@") {
		return Credential{}, errors.New("username must include the Proxmox realm, for example user@pve or root@pam")
	}

	if usesToken {
		in.Username = ""
		in.Password = ""
	} else {
		in.APITokenID = ""
		in.APITokenSecret = ""
	}

	passwordRef, tokenSecretRef, err := s.storeCredentialSecrets(context.Background(), id, in)
	if err != nil {
		return Credential{}, err
	}

	var out Credential
	err = s.db.QueryRow(
		`UPDATE proxmox_credentials
		 SET name = $2,
		     endpoint = $3,
		     username = NULLIF($4, ''),
		     password = NULL,
		     password_secret_ref = NULLIF($5, ''),
		     api_token_id = NULLIF($6, ''),
		     api_token_secret = NULL,
		     api_token_secret_ref = NULLIF($7, ''),
		     insecure_tls = $8
		 WHERE id = $1
		 RETURNING id, name, endpoint, COALESCE(username, ''), COALESCE(api_token_id, ''), insecure_tls, created_at`,
		id,
		in.Name,
		in.Endpoint,
		in.Username,
		passwordRef,
		in.APITokenID,
		tokenSecretRef,
		in.InsecureTLS,
	).Scan(&out.ID, &out.Name, &out.Endpoint, &out.Username, &out.APITokenID, &out.InsecureTLS, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrCredentialNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("update proxmox credential: %w", err)
	}

	return out, nil
}

func (s *Service) DeleteCredential(id int64) error {
	var inUseCount int
	err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM clusters WHERE proxmox_credential_id = $1) + (SELECT COUNT(*) FROM vps_instances WHERE proxmox_credential_id = $1)`, id).Scan(&inUseCount)
	if err != nil {
		return fmt.Errorf("check proxmox credential usage: %w", err)
	}
	if inUseCount > 0 {
		return fmt.Errorf("%w (%d infrastructure record(s) still reference it)", ErrCredentialInUse, inUseCount)
	}

	res, err := s.db.Exec(`DELETE FROM proxmox_credentials WHERE id = $1`, id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23503" {
			return ErrCredentialInUse
		}
		return fmt.Errorf("delete proxmox credential: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// GetCredentialWithSecrets returns the credential including the password and
// API token secret fields (normally omitted from list responses).
func (s *Service) GetCredentialWithSecrets(id int64) (Credential, error) {
	var c Credential
	var passwordRef, tokenSecretRef string
	err := s.db.QueryRow(
		`SELECT id, name, endpoint,
		        COALESCE(username, ''), COALESCE(password, ''),
		        COALESCE(password_secret_ref, ''),
		        COALESCE(api_token_id, ''), COALESCE(api_token_secret, ''),
		        COALESCE(api_token_secret_ref, ''),
		        insecure_tls
		 FROM proxmox_credentials
		 WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.Endpoint,
		&c.Username, &c.Password, &passwordRef,
		&c.APITokenID, &c.APITokenSecret, &tokenSecretRef,
		&c.InsecureTLS)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrCredentialNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get proxmox credential: %w", err)
	}
	if err := s.hydrateCredentialSecrets(context.Background(), &c, passwordRef, tokenSecretRef); err != nil {
		return Credential{}, err
	}
	return c, nil
}

func (s *Service) ControlVMByName(cred Credential, proxmoxNodeName string, vmName string, action string) (string, error) {
	return s.client.ControlVMByName(cred, proxmoxNodeName, vmName, action)
}

func (s *Service) GetVMStatusByName(cred Credential, proxmoxNodeName string, vmName string) (string, error) {
	return s.client.GetVMStatusByName(cred, proxmoxNodeName, vmName)
}

func (s *Service) RunVMGuestCommandByName(cred Credential, proxmoxNodeName string, vmName string, command []string) (string, string, error) {
	return s.client.RunVMGuestCommandByName(cred, proxmoxNodeName, vmName, command)
}

func (s *Service) SetVMHostnameByName(cred Credential, proxmoxNodeName string, vmName string, hostname string) error {
	return s.client.SetVMHostnameByName(cred, proxmoxNodeName, vmName, hostname)
}

func (s *Service) SetVMUserPasswordByName(cred Credential, proxmoxNodeName string, vmName string, username string, password string) error {
	return s.client.SetVMUserPasswordByName(cred, proxmoxNodeName, vmName, username, password)
}

func (s *Service) VMConsoleURLByName(cred Credential, proxmoxNodeName string, vmName string) (VMConsole, error) {
	return s.client.VMConsoleURLByName(cred, proxmoxNodeName, vmName)
}

func (s *Service) CreateVMConsoleSessionByName(cred Credential, proxmoxNodeName string, vmName string) (VMConsoleSession, error) {
	return s.client.CreateVMConsoleSessionByName(cred, proxmoxNodeName, vmName)
}

func (s *Service) ProxyVMConsoleSession(cred Credential, session VMConsoleSession, w http.ResponseWriter, r *http.Request) error {
	return s.client.ProxyVMConsoleSession(cred, session, w, r)
}

func (s *Service) ProxyVMConsoleByName(cred Credential, proxmoxNodeName string, vmName string, w http.ResponseWriter, r *http.Request) error {
	return s.client.ProxyVMConsoleByName(cred, proxmoxNodeName, vmName, w, r)
}

func (s *Service) ListStorageContent(cred Credential, proxmoxNodeName string, storageID string, content string) ([]StorageContent, error) {
	return s.client.ListStorageContent(cred, proxmoxNodeName, storageID, content)
}

func (s *Service) GetTaskStatus(cred Credential, proxmoxNodeName string, upid string) (TaskStatus, error) {
	return s.client.GetTaskStatus(cred, proxmoxNodeName, upid)
}

func (s *Service) DownloadStorageContent(cred Credential, proxmoxNodeName string, storageID string, volID string, destinationPath string) error {
	return s.client.DownloadStorageContent(cred, proxmoxNodeName, storageID, volID, destinationPath)
}

func (s *Service) ListNodeStorage(cred Credential, node Node) ([]NodeStorage, error) {
	items, err := s.client.ListNodeStorage(cred, node.NodeName)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].CredentialID = node.CredentialID
		items[i].CredentialName = node.CredentialName
		items[i].NodeName = node.NodeName
		items[i].LastDiscoveredAt = node.LastDiscoveredAt.Format(time.RFC3339)
	}
	return items, nil
}

func (s *Service) ListStorageInventory() (StorageInventory, error) {
	nodes, err := s.ListEnabledNodes()
	if err != nil {
		return StorageInventory{}, err
	}

	out := StorageInventory{
		Items:  make([]NodeStorage, 0),
		Errors: make([]StorageInventoryError, 0),
	}
	credentials := map[int64]Credential{}
	for _, node := range nodes {
		cred, ok := credentials[node.CredentialID]
		if !ok {
			cred, err = s.GetCredentialWithSecrets(node.CredentialID)
			if err != nil {
				out.Errors = append(out.Errors, StorageInventoryError{
					CredentialID: node.CredentialID,
					NodeName:     node.NodeName,
					Error:        err.Error(),
				})
				continue
			}
			credentials[node.CredentialID] = cred
		}
		items, err := s.ListNodeStorage(cred, node)
		if err != nil {
			out.Errors = append(out.Errors, StorageInventoryError{
				CredentialID: node.CredentialID,
				NodeName:     node.NodeName,
				Error:        err.Error(),
			})
			continue
		}
		out.Items = append(out.Items, items...)
	}
	return out, nil
}

func (s *Service) ControlContainerByName(cred Credential, proxmoxNodeName string, containerName string, action string) (string, error) {
	return s.client.ControlContainerByName(cred, proxmoxNodeName, containerName, action)
}

func (s *Service) GetContainerStatusByName(cred Credential, proxmoxNodeName string, containerName string) (string, error) {
	return s.client.GetContainerStatusByName(cred, proxmoxNodeName, containerName)
}

func (s *Service) ApplyVMFirewallByName(cred Credential, proxmoxNodeName string, vmName string, rules []FirewallRuleSpec, managedPrefix string) (string, error) {
	return s.client.ApplyVMFirewallByName(cred, proxmoxNodeName, vmName, rules, managedPrefix)
}

func (s *Service) ApplyContainerFirewallByName(cred Credential, proxmoxNodeName string, containerName string, rules []FirewallRuleSpec, managedPrefix string) (string, error) {
	return s.client.ApplyContainerFirewallByName(cred, proxmoxNodeName, containerName, rules, managedPrefix)
}

func (s *Service) ClearVMFirewallByName(cred Credential, proxmoxNodeName string, vmName string, managedPrefix string) (string, error) {
	return s.client.ClearVMFirewallByName(cred, proxmoxNodeName, vmName, managedPrefix)
}

func (s *Service) ClearContainerFirewallByName(cred Credential, proxmoxNodeName string, containerName string, managedPrefix string) (string, error) {
	return s.client.ClearContainerFirewallByName(cred, proxmoxNodeName, containerName, managedPrefix)
}

func (s *Service) BackupVMByName(cred Credential, proxmoxNodeName string, vmName string, storageID string, mode string) (string, error) {
	return s.client.BackupVMByName(cred, proxmoxNodeName, vmName, storageID, mode)
}

func (s *Service) BackupContainerByName(cred Credential, proxmoxNodeName string, containerName string, storageID string, mode string) (string, error) {
	return s.client.BackupContainerByName(cred, proxmoxNodeName, containerName, storageID, mode)
}

func (s *Service) ListCredentials() []Credential {
	rows, err := s.db.Query(
		`SELECT id, name, endpoint, COALESCE(username, ''), COALESCE(api_token_id, ''), insecure_tls, created_at
		 FROM proxmox_credentials
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return []Credential{}
	}
	defer rows.Close()

	out := make([]Credential, 0)
	for rows.Next() {
		var c Credential
		if scanErr := rows.Scan(&c.ID, &c.Name, &c.Endpoint, &c.Username, &c.APITokenID, &c.InsecureTLS, &c.CreatedAt); scanErr != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (s *Service) ListNodes() []Node {
	rows, err := s.db.Query(
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.cpu_usage, n.memory_total_bytes, n.memory_used_bytes, n.disk_total_bytes, n.disk_used_bytes, n.enabled, n.last_discovered_at
		 FROM proxmox_nodes n
		 JOIN proxmox_credentials c ON c.id = n.credential_id
		 ORDER BY n.id ASC`,
	)
	if err != nil {
		return []Node{}
	}
	defer rows.Close()

	out := make([]Node, 0)
	for rows.Next() {
		var n Node
		if scanErr := rows.Scan(
			&n.ID,
			&n.CredentialID,
			&n.CredentialName,
			&n.NodeName,
			&n.Status,
			&n.CPUCount,
			&n.CPUUsage,
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
			&n.DiskTotalBytes,
			&n.DiskUsedBytes,
			&n.Enabled,
			&n.LastDiscoveredAt,
		); scanErr != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (s *Service) ListEnabledNodes() ([]Node, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.cpu_usage, n.memory_total_bytes, n.memory_used_bytes, n.disk_total_bytes, n.disk_used_bytes, n.enabled, n.last_discovered_at
		 FROM proxmox_nodes n
		 JOIN proxmox_credentials c ON c.id = n.credential_id
		 WHERE n.enabled = TRUE
		 ORDER BY n.node_name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query enabled proxmox nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		var n Node
		if scanErr := rows.Scan(
			&n.ID,
			&n.CredentialID,
			&n.CredentialName,
			&n.NodeName,
			&n.Status,
			&n.CPUCount,
			&n.CPUUsage,
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
			&n.DiskTotalBytes,
			&n.DiskUsedBytes,
			&n.Enabled,
			&n.LastDiscoveredAt,
		); scanErr != nil {
			continue
		}
		nodes = append(nodes, n)
	}

	return nodes, nil
}

func (s *Service) SetNodeEnabled(nodeID int64, enabled bool) error {
	res, err := s.db.Exec(`UPDATE proxmox_nodes SET enabled = $2 WHERE id = $1`, nodeID, enabled)
	if err != nil {
		return fmt.Errorf("update node enabled flag: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("proxmox node not found")
	}
	return nil
}

// RefreshAllNodes discovers and syncs nodes for all configured credentials.
// It continues on individual credential failures and returns aggregate counts.
func (s *Service) RefreshAllNodes() (successful int, failed int) {
	credentials := s.ListCredentials()
	for _, credential := range credentials {
		if _, err := s.DiscoverAndSyncNodes(credential.ID); err != nil {
			failed++
			continue
		}
		successful++
	}

	return successful, failed
}

func (s *Service) DiscoverAndSyncNodes(credentialID int64) ([]Node, error) {
	credential, err := s.getCredentialForDiscovery(credentialID)
	if err != nil {
		return nil, err
	}

	discovered, err := s.client.DiscoverNodes(credential)
	if err != nil {
		if markErr := s.markCredentialNodesOffline(credentialID); markErr != nil {
			return nil, fmt.Errorf("%w (also failed to mark nodes offline: %v)", err, markErr)
		}
		return nil, err
	}

	for _, d := range discovered {
		_, execErr := s.db.Exec(
			`INSERT INTO proxmox_nodes (credential_id, node_name, status, cpu_count, cpu_usage, memory_total_bytes, memory_used_bytes, disk_total_bytes, disk_used_bytes, enabled, last_discovered_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, NOW())
		 ON CONFLICT (credential_id, node_name)
		 DO UPDATE SET
		     status = EXCLUDED.status,
		     cpu_count = EXCLUDED.cpu_count,
		     cpu_usage = EXCLUDED.cpu_usage,
		     memory_total_bytes = EXCLUDED.memory_total_bytes,
		     memory_used_bytes = EXCLUDED.memory_used_bytes,
		     disk_total_bytes = EXCLUDED.disk_total_bytes,
		     disk_used_bytes = EXCLUDED.disk_used_bytes,
		     last_discovered_at = NOW()`,
			credentialID,
			d.NodeName,
			d.Status,
			d.CPUCount,
			d.CPUUsage,
			d.MemoryTotalBytes,
			d.MemoryUsedBytes,
			d.DiskTotalBytes,
			d.DiskUsedBytes,
		)
		if execErr != nil {
			return nil, fmt.Errorf("upsert discovered node: %w", execErr)
		}
	}

	return s.listNodesByCredential(credentialID)
}

func (s *Service) markCredentialNodesOffline(credentialID int64) error {
	_, err := s.db.Exec(
		`UPDATE proxmox_nodes
		 SET status = 'offline'
		 WHERE credential_id = $1`,
		credentialID,
	)
	if err != nil {
		return fmt.Errorf("mark credential nodes offline: %w", err)
	}
	return nil
}

func (s *Service) getCredentialForDiscovery(id int64) (Credential, error) {
	var c Credential
	var passwordRef, tokenSecretRef string
	err := s.db.QueryRow(
		`SELECT id, name, endpoint,
		        COALESCE(username, ''), COALESCE(password, ''),
		        COALESCE(password_secret_ref, ''),
		        COALESCE(api_token_id, ''), COALESCE(api_token_secret, ''),
		        COALESCE(api_token_secret_ref, ''),
		        insecure_tls, created_at
		 FROM proxmox_credentials
		 WHERE id = $1`,
		id,
	).Scan(&c.ID, &c.Name, &c.Endpoint, &c.Username, &c.Password, &passwordRef, &c.APITokenID, &c.APITokenSecret, &tokenSecretRef, &c.InsecureTLS, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Credential{}, ErrCredentialNotFound
		}
		return Credential{}, fmt.Errorf("fetch proxmox credential: %w", err)
	}
	if err := s.hydrateCredentialSecrets(context.Background(), &c, passwordRef, tokenSecretRef); err != nil {
		return Credential{}, err
	}
	return c, nil
}

func (s *Service) storeCredentialSecrets(ctx context.Context, id int64, in Credential) (string, string, error) {
	if s.secrets == nil {
		return "", "", errors.New("secret store is not configured")
	}
	scope := "proxmox/credentials"
	if id > 0 {
		scope = fmt.Sprintf("%s/%d", scope, id)
	}
	var passwordRef, tokenSecretRef string
	var err error
	if in.Password != "" {
		passwordRef, err = s.secrets.Put(ctx, scope, "password", []byte(in.Password))
		if err != nil {
			return "", "", fmt.Errorf("store proxmox password: %w", err)
		}
	}
	if in.APITokenSecret != "" {
		tokenSecretRef, err = s.secrets.Put(ctx, scope, "api-token-secret", []byte(in.APITokenSecret))
		if err != nil {
			return "", "", fmt.Errorf("store proxmox api token secret: %w", err)
		}
	}
	return passwordRef, tokenSecretRef, nil
}

func (s *Service) hydrateCredentialSecrets(ctx context.Context, c *Credential, passwordRef, tokenSecretRef string) error {
	if strings.TrimSpace(passwordRef) != "" {
		value, err := s.secrets.Get(ctx, passwordRef)
		if err != nil {
			return fmt.Errorf("load proxmox password secret: %w", err)
		}
		c.Password = string(value)
	}
	if strings.TrimSpace(tokenSecretRef) != "" {
		value, err := s.secrets.Get(ctx, tokenSecretRef)
		if err != nil {
			return fmt.Errorf("load proxmox api token secret: %w", err)
		}
		c.APITokenSecret = string(value)
	}
	return nil
}

func (s *Service) listNodesByCredential(credentialID int64) ([]Node, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.cpu_usage, n.memory_total_bytes, n.memory_used_bytes, n.disk_total_bytes, n.disk_used_bytes, n.enabled, n.last_discovered_at
		 FROM proxmox_nodes n
		 JOIN proxmox_credentials c ON c.id = n.credential_id
		 WHERE n.credential_id = $1
		 ORDER BY n.node_name ASC`,
		credentialID,
	)
	if err != nil {
		return nil, fmt.Errorf("list nodes by credential: %w", err)
	}
	defer rows.Close()

	out := make([]Node, 0)
	for rows.Next() {
		var n Node
		if scanErr := rows.Scan(
			&n.ID,
			&n.CredentialID,
			&n.CredentialName,
			&n.NodeName,
			&n.Status,
			&n.CPUCount,
			&n.CPUUsage,
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
			&n.DiskTotalBytes,
			&n.DiskUsedBytes,
			&n.Enabled,
			&n.LastDiscoveredAt,
		); scanErr != nil {
			continue
		}
		out = append(out, n)
	}

	return out, nil
}
