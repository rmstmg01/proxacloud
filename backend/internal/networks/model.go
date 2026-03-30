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
	ErrNetworkNotFound = errors.New("network profile not found")
)

type Profile struct {
	ID        int64     `json:"id"`
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

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(profile Profile) (Profile, error) {
	if err := validateProfile(profile); err != nil {
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
		`INSERT INTO network_profiles (name, cidr, gateway, dns, bridge, vlan, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.CIDR),
		strings.TrimSpace(profile.Gateway),
		pq.Array(normalizeDNS(profile.DNS)),
		strings.TrimSpace(profile.Bridge),
		profile.VLAN,
		profile.IsDefault,
	).Scan(
		&profile.ID,
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
		`SELECT id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at
		 FROM network_profiles
		 ORDER BY id ASC`,
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
		`SELECT id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at
		 FROM network_profiles
		 WHERE id = $1`,
		id,
	).Scan(
		&p.ID,
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

	var updated Profile
	err := s.db.QueryRow(
		`UPDATE network_profiles
		 SET name = $2,
		     cidr = $3,
		     gateway = $4,
		     dns = $5,
		     bridge = $6,
		     vlan = $7,
		     updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		id,
		strings.TrimSpace(updates.Name),
		strings.TrimSpace(updates.CIDR),
		strings.TrimSpace(updates.Gateway),
		pq.Array(normalizeDNS(updates.DNS)),
		strings.TrimSpace(updates.Bridge),
		updates.VLAN,
	).Scan(
		&updated.ID,
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
		 RETURNING id, name, cidr, gateway, dns, bridge, vlan, is_default, created_at, updated_at`,
		id,
	).Scan(
		&updated.ID,
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
