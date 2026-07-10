package networks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNetworkNotFound    = errors.New("network profile not found")
	ErrVPCNotFound        = errors.New("vpc not found")
	ErrReservedIPNotFound = errors.New("reserved ip not found")
	ErrNetworkInUse       = errors.New("network profile is in use")
	ErrVPCInUse           = errors.New("vpc has subnets")
)

type VPC struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	CIDR        string    `json:"cidr"`
	Description string    `json:"description"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Profile struct {
	ID        int64     `json:"id"`
	VPCID     int64     `json:"vpc_id"`
	VPCName   string    `json:"vpc_name,omitempty"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	Gateway   string    `json:"gateway"`
	DNS       []string  `json:"dns"`
	Bridge    string    `json:"bridge"`
	VLAN      int       `json:"vlan"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NodeNetworkTag struct {
	ProxmoxNodeName  string    `json:"proxmox_node_name"`
	NetworkProfileID int64     `json:"network_profile_id"`
	NetworkName      string    `json:"network_name"`
	CIDR             string    `json:"cidr"`
	Gateway          string    `json:"gateway"`
	Bridge           string    `json:"bridge"`
	VLAN             int       `json:"vlan"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ReservedIP struct {
	ID               int64     `json:"id"`
	NetworkProfileID int64     `json:"network_profile_id"`
	NetworkName      string    `json:"network_name"`
	Hostname         string    `json:"hostname"`
	IPAddress        string    `json:"ip_address"`
	Description      string    `json:"description"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type IPUsageEntry struct {
	IPAddress  string     `json:"ip_address"`
	Status     string     `json:"status"`
	SourceType string     `json:"source_type"`
	SourceName string     `json:"source_name"`
	ClusterID  int64      `json:"cluster_id,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Details    string     `json:"details,omitempty"`
}

type IPUsageSummary struct {
	NetworkProfileID int64          `json:"network_profile_id"`
	CIDR             string         `json:"cidr"`
	Gateway          string         `json:"gateway"`
	TotalUsable      int            `json:"total_usable"`
	UsedCount        int            `json:"used_count"`
	ReservedCount    int            `json:"reserved_count"`
	DetectedCount    int            `json:"detected_count"`
	AvailableCount   int            `json:"available_count"`
	ConflictCount    int            `json:"conflict_count"`
	LastScanAt       *time.Time     `json:"last_scan_at,omitempty"`
	Entries          []IPUsageEntry `json:"entries"`
	AvailablePreview []string       `json:"available_preview"`
}

type IPScanResponse struct {
	NetworkProfileID int            `json:"network_profile_id"`
	ScannedCount     int            `json:"scanned_count"`
	DetectedCount    int            `json:"detected_count"`
	StartedAt        time.Time      `json:"started_at"`
	FinishedAt       time.Time      `json:"finished_at"`
	Warnings         []string       `json:"warnings"`
	Usage            IPUsageSummary `json:"usage"`
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) CreateVPC(vpc VPC) (VPC, error) {
	if err := validateVPC(vpc); err != nil {
		return VPC{}, err
	}

	if vpc.IsDefault {
		if _, err := s.db.Exec(`UPDATE vpcs SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return VPC{}, fmt.Errorf("clear default vpc: %w", err)
		}
	}

	var hasDefault bool
	_ = s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM vpcs WHERE is_default = TRUE)`).Scan(&hasDefault)
	if !hasDefault {
		vpc.IsDefault = true
	}

	err := s.db.QueryRow(
		`INSERT INTO vpcs (name, cidr, description, is_default)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, cidr, description, is_default, created_at, updated_at`,
		strings.TrimSpace(vpc.Name),
		strings.TrimSpace(vpc.CIDR),
		strings.TrimSpace(vpc.Description),
		vpc.IsDefault,
	).Scan(&vpc.ID, &vpc.Name, &vpc.CIDR, &vpc.Description, &vpc.IsDefault, &vpc.CreatedAt, &vpc.UpdatedAt)
	if err != nil {
		return VPC{}, fmt.Errorf("insert vpc: %w", err)
	}

	return vpc, nil
}

func (s *Service) ListVPCs() []VPC {
	rows, err := s.db.Query(
		`SELECT id, name, cidr, description, is_default, created_at, updated_at
		 FROM vpcs
		 ORDER BY is_default DESC, name ASC`,
	)
	if err != nil {
		return []VPC{}
	}
	defer rows.Close()

	out := make([]VPC, 0)
	for rows.Next() {
		var vpc VPC
		if scanErr := rows.Scan(&vpc.ID, &vpc.Name, &vpc.CIDR, &vpc.Description, &vpc.IsDefault, &vpc.CreatedAt, &vpc.UpdatedAt); scanErr != nil {
			continue
		}
		out = append(out, vpc)
	}

	return out
}

func (s *Service) UpdateVPC(id int64, updates VPC) (VPC, error) {
	if id <= 0 {
		return VPC{}, ErrVPCNotFound
	}
	if err := validateVPC(updates); err != nil {
		return VPC{}, err
	}
	var updated VPC
	err := s.db.QueryRow(
		`UPDATE vpcs
		    SET name = $2,
		        cidr = $3,
		        description = $4,
		        updated_at = NOW()
		  WHERE id = $1
		  RETURNING id, name, cidr, description, is_default, created_at, updated_at`,
		id,
		strings.TrimSpace(updates.Name),
		strings.TrimSpace(updates.CIDR),
		strings.TrimSpace(updates.Description),
	).Scan(&updated.ID, &updated.Name, &updated.CIDR, &updated.Description, &updated.IsDefault, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VPC{}, ErrVPCNotFound
		}
		return VPC{}, fmt.Errorf("update vpc: %w", err)
	}
	return updated, nil
}

func (s *Service) DeleteVPC(id int64) error {
	if id <= 0 {
		return ErrVPCNotFound
	}
	var subnetCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM network_profiles WHERE vpc_id = $1`, id).Scan(&subnetCount); err != nil {
		return fmt.Errorf("count vpc subnets: %w", err)
	}
	if subnetCount > 0 {
		return ErrVPCInUse
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var wasDefault bool
	res := tx.QueryRow(`DELETE FROM vpcs WHERE id = $1 RETURNING is_default`, id)
	if err := res.Scan(&wasDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVPCNotFound
		}
		return fmt.Errorf("delete vpc: %w", err)
	}
	if wasDefault {
		_, _ = tx.Exec(`UPDATE vpcs SET is_default = TRUE WHERE id = (SELECT id FROM vpcs ORDER BY id ASC LIMIT 1)`)
	}
	return tx.Commit()
}

func (s *Service) Create(profile Profile) (Profile, error) {
	if err := validateProfile(profile); err != nil {
		return Profile{}, err
	}
	if profile.VPCID <= 0 {
		defaultVPCID, err := s.defaultVPCID()
		if err != nil {
			return Profile{}, err
		}
		profile.VPCID = defaultVPCID
	} else if _, err := s.GetVPCByID(profile.VPCID); err != nil {
		return Profile{}, err
	}

	if profile.IsDefault {
		if _, err := s.db.Exec(`UPDATE network_profiles SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
			return Profile{}, fmt.Errorf("clear default profile: %w", err)
		}
	}

	var isDefault bool
	_ = s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM network_profiles WHERE is_default = TRUE)`).Scan(&isDefault)
	if !isDefault {
		profile.IsDefault = true
	}

	err := s.db.QueryRow(
		`INSERT INTO network_profiles (vpc_id, name, cidr, gateway, dns, bridge, vlan, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, vpc_id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		profile.VPCID,
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.CIDR),
		strings.TrimSpace(profile.Gateway),
		pq.Array(normalizeDNS(profile.DNS)),
		strings.TrimSpace(profile.Bridge),
		profile.VLAN,
		profile.IsDefault,
	).Scan(
		&profile.ID,
		&profile.VPCID,
		&profile.Name,
		&profile.CIDR,
		&profile.Gateway,
		pq.Array(&profile.DNS),
		&profile.Bridge,
		&profile.VLAN,
		&profile.IsDefault,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		return Profile{}, fmt.Errorf("insert network profile: %w", err)
	}

	return profile, nil
}

func (s *Service) List() []Profile {
	rows, err := s.db.Query(
		`SELECT p.id, p.vpc_id, COALESCE(v.name, ''), p.name, p.cidr, p.gateway, p.dns, p.bridge, p.vlan, p.is_default, p.created_at, p.updated_at
		 FROM network_profiles p
		 LEFT JOIN vpcs v ON v.id = p.vpc_id
		 ORDER BY v.is_default DESC, v.name ASC, p.id ASC`,
	)
	if err != nil {
		return []Profile{}
	}
	defer rows.Close()

	out := make([]Profile, 0)
	for rows.Next() {
		var p Profile
		if scanErr := rows.Scan(
			&p.ID,
			&p.VPCID,
			&p.VPCName,
			&p.Name,
			&p.CIDR,
			&p.Gateway,
			pq.Array(&p.DNS),
			&p.Bridge,
			&p.VLAN,
			&p.IsDefault,
			&p.CreatedAt,
			&p.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, p)
	}

	return out
}

func (s *Service) GetByID(id int64) (Profile, error) {
	var p Profile
	err := s.db.QueryRow(
		`SELECT p.id, p.vpc_id, COALESCE(v.name, ''), p.name, p.cidr, p.gateway, p.dns, p.bridge, p.vlan, p.is_default, p.created_at, p.updated_at
		 FROM network_profiles p
		 LEFT JOIN vpcs v ON v.id = p.vpc_id
		 WHERE p.id = $1`,
		id,
	).Scan(
		&p.ID,
		&p.VPCID,
		&p.VPCName,
		&p.Name,
		&p.CIDR,
		&p.Gateway,
		pq.Array(&p.DNS),
		&p.Bridge,
		&p.VLAN,
		&p.IsDefault,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNetworkNotFound
		}
		return Profile{}, fmt.Errorf("get network profile: %w", err)
	}

	return p, nil
}

func (s *Service) Update(id int64, updates Profile) (Profile, error) {
	if err := validateProfile(updates); err != nil {
		return Profile{}, err
	}
	if updates.VPCID <= 0 {
		current, err := s.GetByID(id)
		if err != nil {
			return Profile{}, err
		}
		updates.VPCID = current.VPCID
	} else if _, err := s.GetVPCByID(updates.VPCID); err != nil {
		return Profile{}, err
	}

	var updated Profile
	err := s.db.QueryRow(
		`UPDATE network_profiles
		 SET vpc_id = $2,
		     name = $3,
		     cidr = $4,
		     gateway = $5,
		     dns = $6,
		     bridge = $7,
		     vlan = $8,
		     updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, vpc_id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		id,
		updates.VPCID,
		strings.TrimSpace(updates.Name),
		strings.TrimSpace(updates.CIDR),
		strings.TrimSpace(updates.Gateway),
		pq.Array(normalizeDNS(updates.DNS)),
		strings.TrimSpace(updates.Bridge),
		updates.VLAN,
	).Scan(
		&updated.ID,
		&updated.VPCID,
		&updated.Name,
		&updated.CIDR,
		&updated.Gateway,
		pq.Array(&updated.DNS),
		&updated.Bridge,
		&updated.VLAN,
		&updated.IsDefault,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNetworkNotFound
		}
		return Profile{}, fmt.Errorf("update network profile: %w", err)
	}

	return updated, nil
}

func (s *Service) Delete(id int64) error {
	if id <= 0 {
		return ErrNetworkNotFound
	}
	if err := s.ensureNetworkNotInUse(id); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var wasDefault bool
	err = tx.QueryRow(`DELETE FROM network_profiles WHERE id = $1 RETURNING is_default`, id).Scan(&wasDefault)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNetworkNotFound
		}
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			return ErrNetworkInUse
		}
		return fmt.Errorf("delete network profile: %w", err)
	}
	if wasDefault {
		_, _ = tx.Exec(`UPDATE network_profiles SET is_default = TRUE WHERE id = (SELECT id FROM network_profiles ORDER BY id ASC LIMIT 1)`)
	}
	return tx.Commit()
}

func (s *Service) ensureNetworkNotInUse(id int64) error {
	checks := []struct {
		label string
		query string
	}{
		{"clusters", `SELECT COUNT(*) FROM clusters WHERE network_profile_id = $1`},
		{"database clusters", `SELECT COUNT(*) FROM database_clusters WHERE network_profile_id = $1`},
		{"VPS instances", `SELECT COUNT(*) FROM vps_instances WHERE network_profile_id = $1`},
		{"containers", `SELECT COUNT(*) FROM container_instances WHERE network_profile_id = $1`},
	}
	for _, check := range checks {
		var count int
		if err := s.db.QueryRow(check.query, id).Scan(&count); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "does not exist") {
				continue
			}
			return fmt.Errorf("check %s usage: %w", check.label, err)
		}
		if count > 0 {
			return fmt.Errorf("%w by %d %s", ErrNetworkInUse, count, check.label)
		}
	}
	return nil
}

func (s *Service) GetVPCByID(id int64) (VPC, error) {
	var vpc VPC
	err := s.db.QueryRow(
		`SELECT id, name, cidr, description, is_default, created_at, updated_at
		 FROM vpcs
		 WHERE id = $1`,
		id,
	).Scan(&vpc.ID, &vpc.Name, &vpc.CIDR, &vpc.Description, &vpc.IsDefault, &vpc.CreatedAt, &vpc.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VPC{}, ErrVPCNotFound
		}
		return VPC{}, fmt.Errorf("get vpc: %w", err)
	}
	return vpc, nil
}

func (s *Service) SetDefault(id int64) (Profile, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Profile{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE network_profiles SET is_default = FALSE WHERE is_default = TRUE`); err != nil {
		return Profile{}, fmt.Errorf("clear default network: %w", err)
	}

	var updated Profile
	err = tx.QueryRow(
		`UPDATE network_profiles
		 SET is_default = TRUE,
		     updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, vpc_id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		id,
	).Scan(
		&updated.ID,
		&updated.VPCID,
		&updated.Name,
		&updated.CIDR,
		&updated.Gateway,
		pq.Array(&updated.DNS),
		&updated.Bridge,
		&updated.VLAN,
		&updated.IsDefault,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNetworkNotFound
		}
		return Profile{}, fmt.Errorf("set default network: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Profile{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

func (s *Service) ListNodeTags() ([]NodeNetworkTag, error) {
	rows, err := s.db.Query(
		`SELECT t.proxmox_node_name, t.network_profile_id,
		        p.name, p.cidr, p.gateway, p.bridge, p.vlan,
		        t.created_at, t.updated_at
		 FROM proxmox_node_network_tags t
		 JOIN network_profiles p ON p.id = t.network_profile_id
		 ORDER BY t.proxmox_node_name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list node network tags: %w", err)
	}
	defer rows.Close()

	out := make([]NodeNetworkTag, 0)
	for rows.Next() {
		var t NodeNetworkTag
		if scanErr := rows.Scan(
			&t.ProxmoxNodeName,
			&t.NetworkProfileID,
			&t.NetworkName,
			&t.CIDR,
			&t.Gateway,
			&t.Bridge,
			&t.VLAN,
			&t.CreatedAt,
			&t.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, t)
	}

	return out, nil
}

func (s *Service) ListNodeTagMap() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT proxmox_node_name, network_profile_id FROM proxmox_node_network_tags`)
	if err != nil {
		return nil, fmt.Errorf("list node network tag map: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var nodeName string
		var profileID int64
		if scanErr := rows.Scan(&nodeName, &profileID); scanErr != nil {
			continue
		}
		nodeName = strings.TrimSpace(nodeName)
		if nodeName != "" && profileID > 0 {
			out[nodeName] = profileID
		}
	}

	return out, nil
}

func (s *Service) UpsertNodeTag(nodeName string, networkProfileID int64) (NodeNetworkTag, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return NodeNetworkTag{}, errors.New("proxmox_node_name is required")
	}
	if networkProfileID <= 0 {
		return NodeNetworkTag{}, errors.New("network_profile_id is required")
	}

	if _, err := s.GetByID(networkProfileID); err != nil {
		if errors.Is(err, ErrNetworkNotFound) {
			return NodeNetworkTag{}, err
		}
		return NodeNetworkTag{}, fmt.Errorf("validate network profile: %w", err)
	}

	_, err := s.db.Exec(
		`INSERT INTO proxmox_node_network_tags (proxmox_node_name, network_profile_id)
		 VALUES ($1, $2)
		 ON CONFLICT (proxmox_node_name)
		 DO UPDATE SET network_profile_id = EXCLUDED.network_profile_id, updated_at = NOW()`,
		nodeName,
		networkProfileID,
	)
	if err != nil {
		return NodeNetworkTag{}, fmt.Errorf("upsert node network tag: %w", err)
	}

	var out NodeNetworkTag
	err = s.db.QueryRow(
		`SELECT t.proxmox_node_name, t.network_profile_id,
		        p.name, p.cidr, p.gateway, p.bridge, p.vlan,
		        t.created_at, t.updated_at
		 FROM proxmox_node_network_tags t
		 JOIN network_profiles p ON p.id = t.network_profile_id
		 WHERE t.proxmox_node_name = $1`,
		nodeName,
	).Scan(
		&out.ProxmoxNodeName,
		&out.NetworkProfileID,
		&out.NetworkName,
		&out.CIDR,
		&out.Gateway,
		&out.Bridge,
		&out.VLAN,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return NodeNetworkTag{}, fmt.Errorf("load node network tag: %w", err)
	}

	return out, nil
}

func (s *Service) DeleteNodeTag(nodeName string) error {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return errors.New("proxmox_node_name is required")
	}

	res, err := s.db.Exec(`DELETE FROM proxmox_node_network_tags WHERE proxmox_node_name = $1`, nodeName)
	if err != nil {
		return fmt.Errorf("delete node network tag: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("node network tag not found")
	}

	return nil
}

func (s *Service) GetIPUsage(networkProfileID int64) (IPUsageSummary, error) {
	profile, err := s.GetByID(networkProfileID)
	if err != nil {
		return IPUsageSummary{}, err
	}
	_, cidrNet, err := net.ParseCIDR(strings.TrimSpace(profile.CIDR))
	if err != nil {
		return IPUsageSummary{}, errors.New("network profile cidr is invalid")
	}

	usable := usableIPv4Addresses(cidrNet)
	entriesByIP := map[string][]IPUsageEntry{}
	addEntry := func(entry IPUsageEntry) {
		ip := normalizeIPv4(entry.IPAddress)
		if ip == "" || !cidrNet.Contains(net.ParseIP(ip).To4()) {
			return
		}
		entry.IPAddress = ip
		entriesByIP[ip] = append(entriesByIP[ip], entry)
	}

	gateway := normalizeIPv4(profile.Gateway)
	if gateway != "" {
		addEntry(IPUsageEntry{IPAddress: gateway, Status: "reserved", SourceType: "gateway", SourceName: "Network gateway"})
	}
	for _, dns := range profile.DNS {
		if ip := normalizeIPv4(dns); ip != "" {
			addEntry(IPUsageEntry{IPAddress: ip, Status: "reserved", SourceType: "dns", SourceName: "DNS server"})
		}
	}
	if err := s.addReservedIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addClusterNodeIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addClusterVIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addMetalLBIPUsage(networkProfileID, cidrNet, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addVPSIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addContainerIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	if err := s.addDatabaseNodeIPUsage(networkProfileID, addEntry); err != nil {
		return IPUsageSummary{}, err
	}
	lastScanAt, err := s.addDetectedIPUsage(networkProfileID, addEntry)
	if err != nil {
		return IPUsageSummary{}, err
	}

	entries := make([]IPUsageEntry, 0)
	usedIPs := map[string]bool{}
	reservedIPs := map[string]bool{}
	detectedIPs := map[string]bool{}
	conflictIPs := map[string]bool{}
	for ip, list := range entriesByIP {
		authoritativeCount := 0
		for _, entry := range list {
			if entry.SourceType != "live_scan" {
				authoritativeCount++
			}
		}
		if authoritativeCount > 1 {
			conflictIPs[ip] = true
		}
		for _, entry := range list {
			if conflictIPs[ip] && entry.SourceType != "live_scan" {
				entry.Status = "conflict"
			}
			switch entry.Status {
			case "used":
				usedIPs[ip] = true
			case "reserved":
				reservedIPs[ip] = true
			case "detected":
				detectedIPs[ip] = true
			case "conflict":
				usedIPs[ip] = true
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		cmp := compareIPv4(entries[i].IPAddress, entries[j].IPAddress)
		if cmp == 0 {
			return entries[i].SourceType < entries[j].SourceType
		}
		return cmp < 0
	})

	occupied := map[string]bool{}
	for ip := range usedIPs {
		occupied[ip] = true
	}
	for ip := range reservedIPs {
		occupied[ip] = true
	}
	for ip := range detectedIPs {
		occupied[ip] = true
	}
	availablePreview := make([]string, 0, 128)
	availableCount := 0
	for _, ip := range usable {
		if occupied[ip] {
			continue
		}
		availableCount++
		if len(availablePreview) < 128 {
			availablePreview = append(availablePreview, ip)
		}
	}

	return IPUsageSummary{
		NetworkProfileID: networkProfileID,
		CIDR:             profile.CIDR,
		Gateway:          profile.Gateway,
		TotalUsable:      len(usable),
		UsedCount:        len(usedIPs),
		ReservedCount:    len(reservedIPs),
		DetectedCount:    len(detectedIPs),
		AvailableCount:   availableCount,
		ConflictCount:    len(conflictIPs),
		LastScanAt:       lastScanAt,
		Entries:          entries,
		AvailablePreview: availablePreview,
	}, nil
}

func (s *Service) ScanIPUsage(networkProfileID int64) (IPScanResponse, error) {
	profile, err := s.GetByID(networkProfileID)
	if err != nil {
		return IPScanResponse{}, err
	}
	_, cidrNet, err := net.ParseCIDR(strings.TrimSpace(profile.CIDR))
	if err != nil {
		return IPScanResponse{}, errors.New("network profile cidr is invalid")
	}
	startedAt := time.Now()
	candidates := usableIPv4Addresses(cidrNet)
	const maxScanSize = 1024
	warnings := []string{}
	if len(candidates) > maxScanSize {
		candidates = candidates[:maxScanSize]
		warnings = append(warnings, fmt.Sprintf("scan limited to first %d usable IPs for large CIDR %s", maxScanSize, profile.CIDR))
	}

	type result struct {
		ip string
		ok bool
	}
	jobs := make(chan string)
	results := make(chan result)
	workers := 64
	if len(candidates) < workers {
		workers = len(candidates)
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				results <- result{ip: ip, ok: pingIPv4(ip)}
			}
		}()
	}
	go func() {
		for _, ip := range candidates {
			jobs <- ip
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	detected := make([]string, 0)
	for res := range results {
		if res.ok {
			detected = append(detected, res.ip)
		}
	}
	sort.Slice(detected, func(i, j int) bool { return compareIPv4(detected[i], detected[j]) < 0 })

	tx, err := s.db.Begin()
	if err != nil {
		return IPScanResponse{}, fmt.Errorf("begin ip scan transaction: %w", err)
	}
	defer tx.Rollback()
	for _, ip := range detected {
		if _, err := tx.Exec(
			`INSERT INTO network_ip_detections (network_profile_id, ip_address, method, first_seen_at, last_seen_at)
			 VALUES ($1, $2, 'icmp', NOW(), NOW())
			 ON CONFLICT (network_profile_id, ip_address)
			 DO UPDATE SET method = EXCLUDED.method, last_seen_at = NOW()`,
			networkProfileID,
			ip,
		); err != nil {
			return IPScanResponse{}, fmt.Errorf("record detected ip %s: %w", ip, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return IPScanResponse{}, fmt.Errorf("commit ip scan results: %w", err)
	}

	usage, err := s.GetIPUsage(networkProfileID)
	if err != nil {
		return IPScanResponse{}, err
	}
	return IPScanResponse{
		NetworkProfileID: int(networkProfileID),
		ScannedCount:     len(candidates),
		DetectedCount:    len(detected),
		StartedAt:        startedAt,
		FinishedAt:       time.Now(),
		Warnings:         warnings,
		Usage:            usage,
	}, nil
}

func (s *Service) GetReservedIPsByNetworkProfileID(networkProfileID int64) ([]ReservedIP, error) {
	if networkProfileID <= 0 {
		return nil, errors.New("network profile id is required")
	}

	rows, err := s.db.Query(
		`SELECT r.id, r.network_profile_id, p.name, r.hostname, r.ip_address, r.description, r.created_at, r.updated_at
		 FROM network_reserved_ips r
		 JOIN network_profiles p ON p.id = r.network_profile_id
		 WHERE r.network_profile_id = $1
		 ORDER BY r.hostname ASC, r.ip_address ASC`,
		networkProfileID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reserved ips: %w", err)
	}
	defer rows.Close()

	out := make([]ReservedIP, 0)
	for rows.Next() {
		var r ReservedIP
		if scanErr := rows.Scan(
			&r.ID,
			&r.NetworkProfileID,
			&r.NetworkName,
			&r.Hostname,
			&r.IPAddress,
			&r.Description,
			&r.CreatedAt,
			&r.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, r)
	}

	return out, nil
}

func (s *Service) CreateReservedIP(networkProfileID int64, hostname, ipAddress, description string) (ReservedIP, error) {
	profile, err := s.GetByID(networkProfileID)
	if err != nil {
		return ReservedIP{}, err
	}

	hostname = strings.TrimSpace(hostname)
	ipAddress = strings.TrimSpace(ipAddress)
	description = strings.TrimSpace(description)

	if hostname == "" {
		return ReservedIP{}, errors.New("hostname is required")
	}
	if ipAddress == "" {
		return ReservedIP{}, errors.New("ip_address is required")
	}

	ip := net.ParseIP(ipAddress).To4()
	if ip == nil {
		return ReservedIP{}, errors.New("ip_address is invalid")
	}

	_, cidrNet, parseErr := net.ParseCIDR(strings.TrimSpace(profile.CIDR))
	if parseErr != nil {
		return ReservedIP{}, errors.New("network profile cidr is invalid")
	}
	if !cidrNet.Contains(ip) {
		return ReservedIP{}, fmt.Errorf("ip_address must be inside network cidr %s", profile.CIDR)
	}

	if ip.Equal(net.ParseIP(profile.Gateway).To4()) {
		return ReservedIP{}, errors.New("ip_address cannot be the network gateway")
	}
	if isNetworkOrBroadcastAddress(cidrNet, ip) {
		return ReservedIP{}, errors.New("ip_address cannot be network or broadcast address")
	}

	var out ReservedIP
	err = s.db.QueryRow(
		`INSERT INTO network_reserved_ips (network_profile_id, hostname, ip_address, description)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, network_profile_id, hostname, ip_address, description, created_at, updated_at`,
		networkProfileID,
		hostname,
		ip.String(),
		description,
	).Scan(
		&out.ID,
		&out.NetworkProfileID,
		&out.Hostname,
		&out.IPAddress,
		&out.Description,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ReservedIP{}, errors.New("reserved ip already exists for this network profile")
		}
		return ReservedIP{}, fmt.Errorf("create reserved ip: %w", err)
	}
	out.NetworkName = profile.Name

	return out, nil
}

func (s *Service) DeleteReservedIP(networkProfileID int64, reservedIPID int64) error {
	if networkProfileID <= 0 {
		return errors.New("network profile id is required")
	}
	if reservedIPID <= 0 {
		return errors.New("reserved ip id is required")
	}

	res, err := s.db.Exec(`DELETE FROM network_reserved_ips WHERE id = $1 AND network_profile_id = $2`, reservedIPID, networkProfileID)
	if err != nil {
		return fmt.Errorf("delete reserved ip: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrReservedIPNotFound
	}

	return nil
}

func (s *Service) addReservedIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(
		`SELECT hostname, ip_address, description
		 FROM network_reserved_ips
		 WHERE network_profile_id = $1`,
		networkProfileID,
	)
	if err != nil {
		return fmt.Errorf("load reserved ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hostname, ip, description string
		if err := rows.Scan(&hostname, &ip, &description); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "reserved", SourceType: "manual_reservation", SourceName: hostname, Details: description})
	}
	return rows.Err()
}

func (s *Service) addClusterNodeIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(
		`SELECT c.id, c.name, n.vm_name, COALESCE(n.ip_address, '')
		 FROM cluster_nodes n
		 JOIN clusters c ON c.id = n.cluster_id
		 WHERE c.network_profile_id = $1 AND COALESCE(n.ip_address, '') <> ''`,
		networkProfileID,
	)
	if err != nil {
		return fmt.Errorf("load cluster node ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var clusterID int64
		var clusterName, vmName, ip string
		if err := rows.Scan(&clusterID, &clusterName, &vmName, &ip); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "used", SourceType: "cluster_node", SourceName: fmt.Sprintf("%s / %s", clusterName, vmName), ClusterID: clusterID})
	}
	return rows.Err()
}

func (s *Service) addClusterVIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(
		`SELECT c.id, c.name, COALESCE(b.control_plane_vip, '')
		 FROM cluster_bootstrap_configs b
		 JOIN clusters c ON c.id = b.cluster_id
		 WHERE c.network_profile_id = $1 AND COALESCE(b.control_plane_vip, '') <> ''`,
		networkProfileID,
	)
	if err != nil {
		return fmt.Errorf("load control plane vip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var clusterID int64
		var clusterName, ip string
		if err := rows.Scan(&clusterID, &clusterName, &ip); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "used", SourceType: "control_plane_vip", SourceName: clusterName + " kube-vip", ClusterID: clusterID})
	}
	return rows.Err()
}

type metalLBPoolUsage struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

func (s *Service) addMetalLBIPUsage(networkProfileID int64, cidrNet *net.IPNet, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(
		`SELECT c.id, c.name, m.address_pools
		 FROM metallb_configs m
		 JOIN clusters c ON c.id = m.cluster_id
		 WHERE c.network_profile_id = $1`,
		networkProfileID,
	)
	if err != nil {
		return fmt.Errorf("load metallb ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var clusterID int64
		var clusterName string
		var raw []byte
		if err := rows.Scan(&clusterID, &clusterName, &raw); err != nil {
			continue
		}
		var pools []metalLBPoolUsage
		if err := json.Unmarshal(raw, &pools); err != nil {
			continue
		}
		for _, pool := range pools {
			for _, item := range pool.Addresses {
				for _, ip := range expandAddressExpression(item, cidrNet, 256) {
					add(IPUsageEntry{
						IPAddress:  ip,
						Status:     "reserved",
						SourceType: "metallb_pool",
						SourceName: fmt.Sprintf("%s / %s", clusterName, pool.Name),
						ClusterID:  clusterID,
						Details:    item,
					})
				}
			}
		}
	}
	return rows.Err()
}

func (s *Service) addVPSIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(`SELECT name, ip_address FROM vps_instances WHERE network_profile_id = $1 AND ip_address <> ''`, networkProfileID)
	if err != nil {
		return fmt.Errorf("load vps ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, ip string
		if err := rows.Scan(&name, &ip); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "used", SourceType: "vps", SourceName: name})
	}
	return rows.Err()
}

func (s *Service) addContainerIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(`SELECT name, ip_address FROM container_instances WHERE network_profile_id = $1 AND ip_address <> ''`, networkProfileID)
	if err != nil {
		return fmt.Errorf("load container ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, ip string
		if err := rows.Scan(&name, &ip); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "used", SourceType: "container", SourceName: name})
	}
	return rows.Err()
}

func (s *Service) addDatabaseNodeIPUsage(networkProfileID int64, add func(IPUsageEntry)) error {
	rows, err := s.db.Query(
		`SELECT d.name, n.vm_name, n.ip_address
		 FROM database_cluster_nodes n
		 JOIN database_clusters d ON d.id = n.database_cluster_id
		 WHERE d.network_profile_id = $1 AND n.ip_address <> ''`,
		networkProfileID,
	)
	if err != nil {
		return fmt.Errorf("load database node ip usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var dbName, vmName, ip string
		if err := rows.Scan(&dbName, &vmName, &ip); err != nil {
			continue
		}
		add(IPUsageEntry{IPAddress: ip, Status: "used", SourceType: "database_node", SourceName: fmt.Sprintf("%s / %s", dbName, vmName)})
	}
	return rows.Err()
}

func (s *Service) addDetectedIPUsage(networkProfileID int64, add func(IPUsageEntry)) (*time.Time, error) {
	rows, err := s.db.Query(
		`SELECT ip_address, method, last_seen_at
		 FROM network_ip_detections
		 WHERE network_profile_id = $1
		 ORDER BY last_seen_at DESC`,
		networkProfileID,
	)
	if err != nil {
		return nil, fmt.Errorf("load detected ip usage: %w", err)
	}
	defer rows.Close()
	var lastScanAt *time.Time
	for rows.Next() {
		var ip, method string
		var lastSeen time.Time
		if err := rows.Scan(&ip, &method, &lastSeen); err != nil {
			continue
		}
		if lastScanAt == nil || lastSeen.After(*lastScanAt) {
			value := lastSeen
			lastScanAt = &value
		}
		value := lastSeen
		add(IPUsageEntry{IPAddress: ip, Status: "detected", SourceType: "live_scan", SourceName: method, LastSeenAt: &value})
	}
	return lastScanAt, rows.Err()
}

func isNetworkOrBroadcastAddress(cidr *net.IPNet, ip net.IP) bool {
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

func normalizeIPv4(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value)).To4()
	if ip == nil {
		return ""
	}
	return ip.String()
}

func usableIPv4Addresses(cidr *net.IPNet) []string {
	if cidr == nil || cidr.IP.To4() == nil {
		return nil
	}
	out := make([]string, 0)
	start := ipv4ToUint32(cidr.IP.To4())
	ones, bits := cidr.Mask.Size()
	if bits != 32 {
		return nil
	}
	size := uint32(1) << uint32(32-ones)
	if ones == 0 {
		return nil
	}
	end := start + size - 1
	for current := start; current <= end; current++ {
		ip := uint32ToIPv4(current)
		if isNetworkOrBroadcastAddress(cidr, ip) {
			if current == end {
				break
			}
			continue
		}
		out = append(out, ip.String())
		if current == end {
			break
		}
	}
	return out
}

func expandAddressExpression(value string, cidrNet *net.IPNet, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		start := net.ParseIP(strings.TrimSpace(parts[0])).To4()
		end := net.ParseIP(strings.TrimSpace(parts[1])).To4()
		if start == nil || end == nil {
			return nil
		}
		startNum := ipv4ToUint32(start)
		endNum := ipv4ToUint32(end)
		if endNum < startNum {
			return nil
		}
		out := make([]string, 0)
		for current := startNum; current <= endNum && len(out) < limit; current++ {
			ip := uint32ToIPv4(current)
			if cidrNet == nil || cidrNet.Contains(ip) {
				out = append(out, ip.String())
			}
			if current == endNum {
				break
			}
		}
		return out
	}
	if strings.Contains(value, "/") {
		_, nested, err := net.ParseCIDR(value)
		if err != nil {
			return nil
		}
		out := make([]string, 0)
		for _, ip := range usableIPv4Addresses(nested) {
			parsed := net.ParseIP(ip).To4()
			if parsed != nil && (cidrNet == nil || cidrNet.Contains(parsed)) {
				out = append(out, ip)
				if len(out) >= limit {
					break
				}
			}
		}
		return out
	}
	ip := net.ParseIP(value).To4()
	if ip == nil || (cidrNet != nil && !cidrNet.Contains(ip)) {
		return nil
	}
	return []string{ip.String()}
}

func compareIPv4(a, b string) int {
	aa := net.ParseIP(a).To4()
	bb := net.ParseIP(b).To4()
	if aa == nil || bb == nil {
		return strings.Compare(a, b)
	}
	av := ipv4ToUint32(aa)
	bv := ipv4ToUint32(bb)
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

func ipv4ToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIPv4(value uint32) net.IP {
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func pingIPv4(ip string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	args := []string{"-c", "1", "-W", "1", ip}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", "1000", ip}
	}
	cmd := exec.CommandContext(ctx, "ping", args...)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func validateProfile(p Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	p.CIDR = strings.TrimSpace(p.CIDR)
	p.Gateway = strings.TrimSpace(p.Gateway)
	p.Bridge = strings.TrimSpace(p.Bridge)

	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Bridge == "" {
		return errors.New("bridge is required")
	}
	if p.VLAN < 0 || p.VLAN > 4094 {
		return errors.New("vlan must be between 0 and 4094")
	}

	_, cidrNet, err := net.ParseCIDR(p.CIDR)
	if err != nil {
		return errors.New("cidr is invalid")
	}

	gw := net.ParseIP(p.Gateway)
	if gw == nil {
		return errors.New("gateway ip is invalid")
	}
	if !cidrNet.Contains(gw) {
		return errors.New("gateway must be within cidr range")
	}

	for _, dns := range normalizeDNS(p.DNS) {
		if net.ParseIP(dns) == nil {
			return errors.New("dns contains invalid ip")
		}
	}

	return nil
}

func validateVPC(vpc VPC) error {
	vpc.Name = strings.TrimSpace(vpc.Name)
	vpc.CIDR = strings.TrimSpace(vpc.CIDR)
	if vpc.Name == "" {
		return errors.New("vpc name is required")
	}
	if _, _, err := net.ParseCIDR(vpc.CIDR); err != nil {
		return errors.New("vpc cidr is invalid")
	}
	return nil
}

func (s *Service) defaultVPCID() (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM vpcs WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("load default vpc: %w", err)
	}

	var created VPC
	err = s.db.QueryRow(
		`INSERT INTO vpcs (name, cidr, description, is_default)
		 VALUES ('default-vpc', '192.168.0.0/16', 'Default VPC for subnet compatibility', TRUE)
		 RETURNING id, name, cidr, description, is_default, created_at, updated_at`,
	).Scan(&created.ID, &created.Name, &created.CIDR, &created.Description, &created.IsDefault, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		return 0, fmt.Errorf("create default vpc: %w", err)
	}
	return created.ID, nil
}

func normalizeDNS(in []string) []string {
	out := make([]string, 0, len(in))
	for _, val := range in {
		trimmed := strings.TrimSpace(val)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
