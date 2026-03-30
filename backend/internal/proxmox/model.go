package proxmox

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrCredentialNotFound = errors.New("proxmox credential not found")
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
	MemoryTotalBytes int64     `json:"memory_total_bytes"`
	MemoryUsedBytes  int64     `json:"memory_used_bytes"`
	Enabled          bool      `json:"enabled"`
	LastDiscoveredAt time.Time `json:"last_discovered_at"`
}

type Service struct {
	db     *sql.DB
	client *Client
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, client: NewClient()}
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

	var out Credential
	err := s.db.QueryRow(
		`INSERT INTO proxmox_credentials
		 (name, endpoint, username, password, api_token_id, api_token_secret, insecure_tls)
		 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7)
		 RETURNING id, name, endpoint, COALESCE(username, ''), COALESCE(api_token_id, ''), insecure_tls, created_at`,
		in.Name,
		in.Endpoint,
		in.Username,
		in.Password,
		in.APITokenID,
		in.APITokenSecret,
		in.InsecureTLS,
	).Scan(&out.ID, &out.Name, &out.Endpoint, &out.Username, &out.APITokenID, &out.InsecureTLS, &out.CreatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("insert proxmox credential: %w", err)
	}

	return out, nil
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
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.memory_total_bytes, n.memory_used_bytes, n.enabled, n.last_discovered_at
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
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
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
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.memory_total_bytes, n.memory_used_bytes, n.enabled, n.last_discovered_at
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
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
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

func (s *Service) DiscoverAndSyncNodes(credentialID int64) ([]Node, error) {
	credential, err := s.getCredentialForDiscovery(credentialID)
	if err != nil {
		return nil, err
	}

	discovered, err := s.client.DiscoverNodes(credential)
	if err != nil {
		return nil, err
	}

	for _, d := range discovered {
		_, execErr := s.db.Exec(
			`INSERT INTO proxmox_nodes (credential_id, node_name, status, cpu_count, memory_total_bytes, memory_used_bytes, enabled, last_discovered_at)
			 VALUES ($1, $2, $3, $4, $5, $6, TRUE, NOW())
			 ON CONFLICT (credential_id, node_name)
			 DO UPDATE SET
			     status = EXCLUDED.status,
			     cpu_count = EXCLUDED.cpu_count,
			     memory_total_bytes = EXCLUDED.memory_total_bytes,
			     memory_used_bytes = EXCLUDED.memory_used_bytes,
			     last_discovered_at = NOW()`,
			credentialID,
			d.NodeName,
			d.Status,
			d.CPUCount,
			d.MemoryTotalBytes,
			d.MemoryUsedBytes,
		)
		if execErr != nil {
			return nil, fmt.Errorf("upsert discovered node: %w", execErr)
		}
	}

	return s.listNodesByCredential(credentialID)
}

func (s *Service) getCredentialForDiscovery(id int64) (Credential, error) {
	var c Credential
	err := s.db.QueryRow(
		`SELECT id, name, endpoint, COALESCE(username, ''), COALESCE(password, ''), COALESCE(api_token_id, ''), COALESCE(api_token_secret, ''), insecure_tls, created_at
		 FROM proxmox_credentials
		 WHERE id = $1`,
		id,
	).Scan(&c.ID, &c.Name, &c.Endpoint, &c.Username, &c.Password, &c.APITokenID, &c.APITokenSecret, &c.InsecureTLS, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Credential{}, ErrCredentialNotFound
		}
		return Credential{}, fmt.Errorf("fetch proxmox credential: %w", err)
	}
	return c, nil
}

func (s *Service) listNodesByCredential(credentialID int64) ([]Node, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.credential_id, c.name, n.node_name, n.status, n.cpu_count, n.memory_total_bytes, n.memory_used_bytes, n.enabled, n.last_discovered_at
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
			&n.MemoryTotalBytes,
			&n.MemoryUsedBytes,
			&n.Enabled,
			&n.LastDiscoveredAt,
		); scanErr != nil {
			continue
		}
		out = append(out, n)
	}

	return out, nil
}
