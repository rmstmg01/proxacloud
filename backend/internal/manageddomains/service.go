package manageddomains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	"proxacloud/backend/internal/secrets"
)

var (
	ErrNotFound = errors.New("managed tunnel domain not found")
	ErrInUse    = errors.New("managed tunnel domain is in use")
	domainRE    = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

type Domain struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	BaseDomain         string    `json:"base_domain"`
	CloudflareZoneID   string    `json:"cloudflare_zone_id"`
	DNSTarget          string    `json:"dns_target"`
	DNSRecordType      string    `json:"dns_record_type"`
	DNSProxied         bool      `json:"dns_proxied"`
	ReservedLabels     []string  `json:"reserved_labels"`
	AvailableForVPS    bool      `json:"available_for_vps"`
	Active             bool      `json:"active"`
	IsDefault          bool      `json:"is_default"`
	HasCloudflareToken bool      `json:"has_cloudflare_token"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	tokenRef           string
}
type Runtime struct {
	Domain
	CloudflareAPIToken string
}
type Input struct {
	Name             string   `json:"name"`
	BaseDomain       string   `json:"base_domain"`
	CloudflareZoneID string   `json:"cloudflare_zone_id"`
	CloudflareToken  string   `json:"cloudflare_api_token"`
	DNSTarget        string   `json:"dns_target"`
	DNSRecordType    string   `json:"dns_record_type"`
	DNSProxied       bool     `json:"dns_proxied"`
	ReservedLabels   []string `json:"reserved_labels"`
	AvailableForVPS  bool     `json:"available_for_vps"`
	Active           bool     `json:"active"`
	IsDefault        bool     `json:"is_default"`
}
type LegacyConfig struct {
	Name, BaseDomain, ZoneID, APIToken, DNSTarget, RecordType string
	Proxied                                                   bool
}
type Service struct {
	db      *sql.DB
	secrets secrets.Store
}

func New(db *sql.DB, secretStore secrets.Store) *Service {
	return &Service{db: db, secrets: secretStore}
}

func (s *Service) EnsureLegacy(ctx context.Context, cfg LegacyConfig) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_tunnel_domains`).Scan(&count); err != nil || count > 0 {
		if err != nil {
			return err
		}
		return s.BackfillReferences(ctx)
	}
	if strings.TrimSpace(cfg.BaseDomain) == "" || strings.TrimSpace(cfg.ZoneID) == "" || strings.TrimSpace(cfg.APIToken) == "" || strings.TrimSpace(cfg.DNSTarget) == "" {
		return nil
	}
	_, err := s.Create(ctx, Input{Name: cfg.Name, BaseDomain: cfg.BaseDomain, CloudflareZoneID: cfg.ZoneID, CloudflareToken: cfg.APIToken, DNSTarget: cfg.DNSTarget, DNSRecordType: cfg.RecordType, DNSProxied: cfg.Proxied, AvailableForVPS: true, Active: true, IsDefault: true})
	if err != nil {
		return err
	}
	return s.BackfillReferences(ctx)
}

func (s *Service) BackfillReferences(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE vps_tunnel_attachments a SET managed_domain_id=(SELECT d.id FROM managed_tunnel_domains d WHERE a.public_hostname=d.base_domain OR a.public_hostname LIKE '%.'||d.base_domain ORDER BY LENGTH(d.base_domain) DESC LIMIT 1), hostname_label=CASE WHEN hostname_label='' THEN SPLIT_PART(a.public_hostname,'.',1) ELSE hostname_label END WHERE managed_domain_id IS NULL AND EXISTS(SELECT 1 FROM managed_tunnel_domains d WHERE a.public_hostname=d.base_domain OR a.public_hostname LIKE '%.'||d.base_domain)`); err != nil {
		return fmt.Errorf("backfill VPS tunnel domains: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE vps_tunnel_http_routes r SET managed_domain_id=(SELECT d.id FROM managed_tunnel_domains d WHERE r.public_hostname=d.base_domain OR r.public_hostname LIKE '%.'||d.base_domain ORDER BY LENGTH(d.base_domain) DESC LIMIT 1) WHERE managed_domain_id IS NULL AND EXISTS(SELECT 1 FROM managed_tunnel_domains d WHERE r.public_hostname=d.base_domain OR r.public_hostname LIKE '%.'||d.base_domain)`); err != nil {
		return fmt.Errorf("backfill VPS route domains: %w", err)
	}
	return nil
}
func (s *Service) List(ctx context.Context, availableOnly bool) ([]Domain, error) {
	query := domainSelect
	if availableOnly {
		query += ` WHERE active=TRUE AND available_for_vps=TRUE`
	}
	query += ` ORDER BY is_default DESC,name,base_domain`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list managed tunnel domains: %w", err)
	}
	defer rows.Close()
	items := []Domain{}
	for rows.Next() {
		item, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) RuntimeByID(ctx context.Context, id int64) (Runtime, error) {
	var row *sql.Row
	if id > 0 {
		row = s.db.QueryRowContext(ctx, domainSelect+` WHERE id=$1 AND active=TRUE AND available_for_vps=TRUE`, id)
	} else {
		row = s.db.QueryRowContext(ctx, domainSelect+` WHERE active=TRUE AND available_for_vps=TRUE ORDER BY is_default DESC,id LIMIT 1`)
	}
	d, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, ErrNotFound
	}
	if err != nil {
		return Runtime{}, err
	}
	return s.runtime(ctx, d)
}
func (s *Service) RuntimeForHostname(ctx context.Context, hostname string) (Runtime, error) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	d, err := scanDomain(s.db.QueryRowContext(ctx, domainSelect+` WHERE active=TRUE AND ($1=base_domain OR $1 LIKE '%.'||base_domain) ORDER BY LENGTH(base_domain) DESC LIMIT 1`, hostname))
	if errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, ErrNotFound
	}
	if err != nil {
		return Runtime{}, err
	}
	return s.runtime(ctx, d)
}
func (s *Service) runtime(ctx context.Context, d Domain) (Runtime, error) {
	if d.tokenRef == "" {
		return Runtime{}, errors.New("managed domain has no Cloudflare credential")
	}
	raw, err := s.secrets.Get(ctx, d.tokenRef)
	if err != nil {
		return Runtime{}, fmt.Errorf("load managed domain credential: %w", err)
	}
	return Runtime{Domain: d, CloudflareAPIToken: string(raw)}, nil
}

func (s *Service) Create(ctx context.Context, in Input) (Domain, error) {
	in, err := validateInput(in)
	if err != nil {
		return Domain{}, err
	}
	if in.CloudflareToken == "" {
		return Domain{}, errors.New("Cloudflare API token is required")
	}
	ref, err := s.secrets.Put(ctx, "managed-tunnel-domain", in.BaseDomain, []byte(in.CloudflareToken))
	if err != nil {
		return Domain{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = s.secrets.Delete(ctx, ref)
		return Domain{}, err
	}
	defer tx.Rollback()
	var hasDefault bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_tunnel_domains WHERE is_default=TRUE)`).Scan(&hasDefault); err != nil {
		_ = s.secrets.Delete(ctx, ref)
		return Domain{}, err
	}
	if !hasDefault {
		in.IsDefault = true
	}
	if in.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE managed_tunnel_domains SET is_default=FALSE WHERE is_default=TRUE`); err != nil {
			_ = s.secrets.Delete(ctx, ref)
			return Domain{}, err
		}
	}
	d, err := scanDomain(tx.QueryRowContext(ctx, `INSERT INTO managed_tunnel_domains(name,base_domain,cloudflare_zone_id,cloudflare_api_token_ref,dns_target,dns_record_type,dns_proxied,reserved_labels,available_for_vps,active,is_default) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+domainColumns, in.Name, in.BaseDomain, in.CloudflareZoneID, ref, in.DNSTarget, in.DNSRecordType, in.DNSProxied, pq.Array(in.ReservedLabels), in.AvailableForVPS, in.Active, in.IsDefault))
	if err != nil {
		_ = s.secrets.Delete(ctx, ref)
		return Domain{}, fmt.Errorf("create managed tunnel domain: %w", err)
	}
	if err = tx.Commit(); err != nil {
		_ = s.secrets.Delete(ctx, ref)
		return Domain{}, err
	}
	return d, nil
}
func (s *Service) Update(ctx context.Context, id int64, in Input) (Domain, error) {
	in, err := validateInput(in)
	if err != nil {
		return Domain{}, err
	}
	current, err := s.get(ctx, id)
	if err != nil {
		return Domain{}, err
	}
	var used bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vps_tunnel_attachments WHERE managed_domain_id=$1 UNION ALL SELECT 1 FROM vps_tunnel_http_routes WHERE managed_domain_id=$1)`, id).Scan(&used); err != nil {
		return Domain{}, err
	}
	if used && (in.BaseDomain != current.BaseDomain || in.CloudflareZoneID != current.CloudflareZoneID || !in.Active) {
		return Domain{}, errors.New("base domain, Cloudflare zone, and active status cannot be changed while tunnels use this domain")
	}
	ref, newRef := current.tokenRef, ""
	if in.CloudflareToken != "" {
		newRef, err = s.secrets.Put(ctx, "managed-tunnel-domain", in.BaseDomain, []byte(in.CloudflareToken))
		if err != nil {
			return Domain{}, err
		}
		ref = newRef
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if newRef != "" {
			_ = s.secrets.Delete(ctx, newRef)
		}
		return Domain{}, err
	}
	defer tx.Rollback()
	if in.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE managed_tunnel_domains SET is_default=FALSE WHERE is_default=TRUE AND id<>$1`, id); err != nil {
			return Domain{}, err
		}
	}
	d, err := scanDomain(tx.QueryRowContext(ctx, `UPDATE managed_tunnel_domains SET name=$2,base_domain=$3,cloudflare_zone_id=$4,cloudflare_api_token_ref=$5,dns_target=$6,dns_record_type=$7,dns_proxied=$8,reserved_labels=$9,available_for_vps=$10,active=$11,is_default=$12,updated_at=NOW() WHERE id=$1 RETURNING `+domainColumns, id, in.Name, in.BaseDomain, in.CloudflareZoneID, ref, in.DNSTarget, in.DNSRecordType, in.DNSProxied, pq.Array(in.ReservedLabels), in.AvailableForVPS, in.Active, in.IsDefault))
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		if newRef != "" {
			_ = s.secrets.Delete(ctx, newRef)
		}
		return Domain{}, err
	}
	if err = tx.Commit(); err != nil {
		return Domain{}, err
	}
	if newRef != "" && current.tokenRef != "" {
		_ = s.secrets.Delete(ctx, current.tokenRef)
	}
	return d, nil
}
func (s *Service) Delete(ctx context.Context, id int64) error {
	d, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	var used bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vps_tunnel_attachments WHERE managed_domain_id=$1 UNION ALL SELECT 1 FROM vps_tunnel_http_routes WHERE managed_domain_id=$1)`, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrInUse
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM managed_tunnel_domains WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	if d.IsDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE managed_tunnel_domains SET is_default=TRUE,updated_at=NOW() WHERE id=(SELECT id FROM managed_tunnel_domains WHERE active=TRUE ORDER BY id LIMIT 1)`)
	}
	if d.tokenRef != "" {
		_ = s.secrets.Delete(ctx, d.tokenRef)
	}
	return nil
}
func (s *Service) get(ctx context.Context, id int64) (Domain, error) {
	d, err := scanDomain(s.db.QueryRowContext(ctx, domainSelect+` WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	return d, err
}

const domainColumns = `id,name,base_domain,cloudflare_zone_id,cloudflare_api_token_ref,dns_target,dns_record_type,dns_proxied,reserved_labels,available_for_vps,active,is_default,created_at,updated_at`
const domainSelect = `SELECT ` + domainColumns + ` FROM managed_tunnel_domains`

type scanner interface{ Scan(...any) error }

func scanDomain(row scanner) (Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.Name, &d.BaseDomain, &d.CloudflareZoneID, &d.tokenRef, &d.DNSTarget, &d.DNSRecordType, &d.DNSProxied, pq.Array(&d.ReservedLabels), &d.AvailableForVPS, &d.Active, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt)
	d.HasCloudflareToken = d.tokenRef != ""
	return d, err
}
func validateInput(in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(in.BaseDomain)), ".")
	in.CloudflareZoneID = strings.TrimSpace(in.CloudflareZoneID)
	in.CloudflareToken = strings.TrimSpace(in.CloudflareToken)
	in.DNSTarget = strings.TrimSuffix(strings.TrimSpace(in.DNSTarget), ".")
	in.DNSRecordType = strings.ToUpper(strings.TrimSpace(in.DNSRecordType))
	if in.Name == "" {
		return in, errors.New("domain name is required")
	}
	if !domainRE.MatchString(in.BaseDomain) {
		return in, errors.New("a valid base domain is required")
	}
	if in.CloudflareZoneID == "" {
		return in, errors.New("Cloudflare zone ID is required")
	}
	if in.DNSTarget == "" {
		return in, errors.New("DNS target is required")
	}
	if in.DNSRecordType == "" {
		in.DNSRecordType = "CNAME"
	}
	if in.DNSRecordType != "A" && in.DNSRecordType != "AAAA" && in.DNSRecordType != "CNAME" {
		return in, errors.New("DNS record type must be A, AAAA, or CNAME")
	}
	seen := map[string]bool{}
	labels := []string{}
	for _, label := range in.ReservedLabels {
		label = strings.Trim(strings.ToLower(strings.TrimSpace(label)), "-")
		if label != "" && !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		labels = []string{"admin", "api", "mail", "merchant", "pos", "www"}
	}
	in.ReservedLabels = labels
	return in, nil
}
func BuildHostname(label string, runtime Runtime) (string, error) {
	label = strings.Trim(strings.ToLower(strings.TrimSpace(label)), "-")
	valid := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	if label == "" || len(label) > 63 || !valid.MatchString(label) {
		return "", errors.New("hostname label must contain letters, numbers, or hyphens")
	}
	for _, reserved := range runtime.ReservedLabels {
		if label == reserved {
			return "", fmt.Errorf("hostname label %s is reserved", label)
		}
	}
	return label + "." + runtime.BaseDomain, nil
}
