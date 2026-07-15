package regions

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrRegionNotFound  = errors.New("region not found")
	ErrRegionInUse     = errors.New("region is in use")
	ErrRegionNoNode    = errors.New("region has no enabled Proxmox node")
	ErrNodeAssigned    = errors.New("Proxmox node is already assigned to another region")
	ErrNodeNotFound    = errors.New("Proxmox node not found")
	ErrNodeNotAssigned = errors.New("Proxmox node is not assigned to this region")
)

type Region struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	IsDefault   bool            `json:"is_default"`
	Nodes       []RegionNode    `json:"nodes"`
	Networks    []RegionNetwork `json:"networks"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type RegionNode struct {
	ID             int64  `json:"id"`
	CredentialID   int64  `json:"credential_id"`
	CredentialName string `json:"credential_name,omitempty"`
	NodeName       string `json:"node_name"`
	Enabled        bool   `json:"enabled"`
	Status         string `json:"status"`
}

type RegionNetwork struct {
	ID      int64  `json:"id"`
	VPCID   int64  `json:"vpc_id"`
	VPCName string `json:"vpc_name,omitempty"`
	Name    string `json:"name"`
	CIDR    string `json:"cidr"`
	Bridge  string `json:"bridge"`
	VLAN    int    `json:"vlan"`
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) List() ([]Region, error) {
	rows, err := s.db.Query(`SELECT id, name, slug, description, enabled, is_default, created_at, updated_at FROM regions ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()

	out := make([]Region, 0)
	for rows.Next() {
		var region Region
		if err := rows.Scan(&region.ID, &region.Name, &region.Slug, &region.Description, &region.Enabled, &region.IsDefault, &region.CreatedAt, &region.UpdatedAt); err != nil {
			return nil, err
		}
		region.Nodes, _ = s.nodes(region.ID)
		region.Networks, _ = s.networks(region.ID)
		out = append(out, region)
	}
	return out, rows.Err()
}

func (s *Service) Create(region Region) (Region, error) {
	if err := validateRegion(&region); err != nil {
		return Region{}, err
	}
	if region.IsDefault {
		if _, err := s.db.Exec(`UPDATE regions SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return Region{}, fmt.Errorf("clear default region: %w", err)
		}
	}
	var out Region
	err := s.db.QueryRow(
		`INSERT INTO regions (name, slug, description, enabled, is_default)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, slug, description, enabled, is_default, created_at, updated_at`,
		region.Name, region.Slug, region.Description, region.Enabled, region.IsDefault,
	).Scan(&out.ID, &out.Name, &out.Slug, &out.Description, &out.Enabled, &out.IsDefault, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Region{}, fmt.Errorf("create region: %w", err)
	}
	return out, nil
}

func (s *Service) Update(id int64, region Region) (Region, error) {
	if id <= 0 {
		return Region{}, ErrRegionNotFound
	}
	if err := validateRegion(&region); err != nil {
		return Region{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Region{}, err
	}
	defer tx.Rollback()

	if region.IsDefault {
		if _, err := tx.Exec(`UPDATE regions SET is_default = FALSE WHERE id <> $1`, id); err != nil {
			return Region{}, fmt.Errorf("clear default region: %w", err)
		}
	}
	var out Region
	err = tx.QueryRow(
		`UPDATE regions
		    SET name = $2, slug = $3, description = $4, enabled = $5, is_default = $6, updated_at = NOW()
		  WHERE id = $1
		  RETURNING id, name, slug, description, enabled, is_default, created_at, updated_at`,
		id, region.Name, region.Slug, region.Description, region.Enabled, region.IsDefault,
	).Scan(&out.ID, &out.Name, &out.Slug, &out.Description, &out.Enabled, &out.IsDefault, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Region{}, ErrRegionNotFound
		}
		return Region{}, fmt.Errorf("update region: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Region{}, err
	}
	out.Nodes, _ = s.nodes(out.ID)
	out.Networks, _ = s.networks(out.ID)
	return out, nil
}

func (s *Service) Delete(id int64) error {
	if id <= 0 {
		return ErrRegionNotFound
	}
	var inUse bool
	if err := s.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM vps_instances WHERE region_id = $1)
		     OR EXISTS (SELECT 1 FROM clusters WHERE region_id = $1)
		     OR EXISTS (SELECT 1 FROM database_clusters WHERE region_id = $1)`,
		id,
	).Scan(&inUse); err != nil {
		return fmt.Errorf("check region use: %w", err)
	}
	if inUse {
		return ErrRegionInUse
	}
	res, err := s.db.Exec(`DELETE FROM regions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete region: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrRegionNotFound
	}
	return nil
}

func (s *Service) SetNodes(regionID int64, nodeIDs []int64) error {
	if err := s.ensureRegion(regionID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	uniqueNodeIDs := uniquePositive(nodeIDs)
	for _, nodeID := range uniqueNodeIDs {
		var ownerRegion string
		err := tx.QueryRow(
			`SELECT r.name
			   FROM region_nodes rn
			   JOIN regions r ON r.id = rn.region_id
			  WHERE rn.proxmox_node_id = $1 AND rn.region_id <> $2
			  LIMIT 1`,
			nodeID, regionID,
		).Scan(&ownerRegion)
		if err == nil {
			return fmt.Errorf("%w: node_id=%d belongs to %s; use move instead", ErrNodeAssigned, nodeID, ownerRegion)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check region node assignment: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM region_nodes WHERE region_id = $1`, regionID); err != nil {
		return err
	}
	for _, nodeID := range uniqueNodeIDs {
		if _, err := tx.Exec(`INSERT INTO region_nodes (region_id, proxmox_node_id) VALUES ($1, $2)`, regionID, nodeID); err != nil {
			if isRegionNodeAssignmentConflict(err) {
				return fmt.Errorf("%w: node_id=%d", ErrNodeAssigned, nodeID)
			}
			return fmt.Errorf("assign region node: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Service) MoveNode(regionID, nodeID int64) error {
	if err := s.ensureRegion(regionID); err != nil {
		return err
	}
	if nodeID <= 0 {
		return ErrNodeNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var nodeExists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM proxmox_nodes WHERE id = $1)`, nodeID).Scan(&nodeExists); err != nil {
		return fmt.Errorf("check Proxmox node: %w", err)
	}
	if !nodeExists {
		return ErrNodeNotFound
	}
	if _, err := tx.Exec(`DELETE FROM region_nodes WHERE proxmox_node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("remove previous region node assignment: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO region_nodes (region_id, proxmox_node_id) VALUES ($1, $2)`, regionID, nodeID); err != nil {
		if isRegionNodeAssignmentConflict(err) {
			return ErrNodeAssigned
		}
		return fmt.Errorf("move region node: %w", err)
	}
	return tx.Commit()
}

func (s *Service) UnassignNode(regionID, nodeID int64) error {
	if err := s.ensureRegion(regionID); err != nil {
		return err
	}
	if nodeID <= 0 {
		return ErrNodeNotFound
	}
	res, err := s.db.Exec(
		`DELETE FROM region_nodes WHERE region_id = $1 AND proxmox_node_id = $2`,
		regionID, nodeID,
	)
	if err != nil {
		return fmt.Errorf("unassign region node: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNodeNotAssigned
	}
	return nil
}

func isRegionNodeAssignmentConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (s *Service) SetNetworks(regionID int64, networkIDs []int64) error {
	if err := s.ensureRegion(regionID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM region_networks WHERE region_id = $1`, regionID); err != nil {
		return err
	}
	for _, networkID := range uniquePositive(networkIDs) {
		if _, err := tx.Exec(`INSERT INTO region_networks (region_id, network_profile_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, regionID, networkID); err != nil {
			return fmt.Errorf("assign region network: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Service) PickNode(regionID int64) (int64, string, error) {
	if regionID <= 0 {
		return 0, "", ErrRegionNotFound
	}
	var credentialID int64
	var nodeName string
	err := s.db.QueryRow(
		`SELECT n.credential_id, n.node_name
		   FROM region_nodes rn
		   JOIN proxmox_nodes n ON n.id = rn.proxmox_node_id
		   JOIN regions r ON r.id = rn.region_id
		  WHERE rn.region_id = $1
		    AND r.enabled = TRUE
		    AND n.enabled = TRUE
		  ORDER BY n.id ASC
		  LIMIT 1`,
		regionID,
	).Scan(&credentialID, &nodeName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrRegionNoNode
		}
		return 0, "", fmt.Errorf("pick region node: %w", err)
	}
	return credentialID, nodeName, nil
}

func (s *Service) NetworkAllowed(regionID int64, networkProfileID int64) (bool, error) {
	if regionID <= 0 || networkProfileID <= 0 {
		return false, nil
	}
	var allowed bool
	err := s.db.QueryRow(
		`SELECT EXISTS (
			SELECT 1
			  FROM region_networks rn
			  JOIN regions r ON r.id = rn.region_id
			 WHERE rn.region_id = $1
			   AND rn.network_profile_id = $2
			   AND r.enabled = TRUE
		)`,
		regionID, networkProfileID,
	).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check region network: %w", err)
	}
	return allowed, nil
}

func (s *Service) nodes(regionID int64) ([]RegionNode, error) {
	rows, err := s.db.Query(
		`SELECT n.id, n.credential_id, COALESCE(c.name, ''), n.node_name, n.enabled, n.status
		   FROM region_nodes rn
		   JOIN proxmox_nodes n ON n.id = rn.proxmox_node_id
		   LEFT JOIN proxmox_credentials c ON c.id = n.credential_id
		  WHERE rn.region_id = $1
		  ORDER BY n.node_name ASC`,
		regionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RegionNode, 0)
	for rows.Next() {
		var node RegionNode
		if err := rows.Scan(&node.ID, &node.CredentialID, &node.CredentialName, &node.NodeName, &node.Enabled, &node.Status); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Service) networks(regionID int64) ([]RegionNetwork, error) {
	rows, err := s.db.Query(
		`SELECT p.id, COALESCE(p.vpc_id, 0), COALESCE(v.name, ''), p.name, p.cidr, p.bridge, p.vlan
		   FROM region_networks rn
		   JOIN network_profiles p ON p.id = rn.network_profile_id
		   LEFT JOIN vpcs v ON v.id = p.vpc_id
		  WHERE rn.region_id = $1
		  ORDER BY p.name ASC`,
		regionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RegionNetwork, 0)
	for rows.Next() {
		var network RegionNetwork
		if err := rows.Scan(&network.ID, &network.VPCID, &network.VPCName, &network.Name, &network.CIDR, &network.Bridge, &network.VLAN); err != nil {
			return nil, err
		}
		out = append(out, network)
	}
	return out, rows.Err()
}

func (s *Service) ensureRegion(id int64) error {
	var ok bool
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM regions WHERE id = $1)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrRegionNotFound
	}
	return nil
}

func validateRegion(region *Region) error {
	region.Name = strings.TrimSpace(region.Name)
	region.Slug = strings.TrimSpace(strings.ToLower(region.Slug))
	region.Description = strings.TrimSpace(region.Description)
	if region.Name == "" {
		return errors.New("name is required")
	}
	if region.Slug == "" {
		region.Slug = slugify(region.Name)
	}
	if region.Slug == "" {
		return errors.New("slug is required")
	}
	return nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniquePositive(ids []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
