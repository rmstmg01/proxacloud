package networks

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNetworkNotFound    = errors.New("network profile not found")
	ErrVPCNotFound        = errors.New("vpc not found")
	ErrReservedIPNotFound = errors.New("reserved ip not found")
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
