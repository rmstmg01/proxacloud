package sshkeys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"proxacloud/backend/internal/secrets"
	"proxacloud/backend/internal/tenant"

	"golang.org/x/crypto/ssh"
)

var ErrKeyPairNotFound = errors.New("ssh key pair not found")

type KeyPair struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	SSHUser    string    `json:"ssh_user"`
	PublicKey  string    `json:"public_key"`
	PrivateKey string    `json:"private_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CustomerPublicKey struct {
	ID                int64     `json:"id"`
	CustomerAccountID int64     `json:"customer_account_id,omitempty"`
	Name              string    `json:"name"`
	SSHUser           string    `json:"ssh_user"`
	PublicKey         string    `json:"public_key"`
	Fingerprint       string    `json:"fingerprint"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Service struct {
	db      *sql.DB
	secrets secrets.Store
}

func NewService(db *sql.DB, secretStore secrets.Store) *Service {
	return &Service{db: db, secrets: secretStore}
}

func (s *Service) Create(in KeyPair) (KeyPair, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.SSHUser = strings.TrimSpace(in.SSHUser)
	in.PublicKey = strings.TrimSpace(in.PublicKey)
	in.PrivateKey = strings.TrimSpace(in.PrivateKey)

	if in.Name == "" {
		return KeyPair{}, errors.New("name is required")
	}
	if in.SSHUser == "" {
		in.SSHUser = "root"
	}
	if in.PublicKey == "" {
		return KeyPair{}, errors.New("public_key is required")
	}
	if in.PrivateKey == "" {
		return KeyPair{}, errors.New("private_key is required")
	}

	privateKeyRef, err := s.secrets.Put(context.Background(), "ssh-key-pairs", in.Name, []byte(in.PrivateKey))
	if err != nil {
		return KeyPair{}, fmt.Errorf("store ssh private key: %w", err)
	}

	var out KeyPair
	err = s.db.QueryRow(
		`INSERT INTO ssh_key_pairs (name, ssh_user, public_key, private_key, private_key_secret_ref)
		 VALUES ($1, $2, $3, NULL, $4)
		 RETURNING id, name, ssh_user, public_key, created_at, updated_at`,
		in.Name,
		in.SSHUser,
		in.PublicKey,
		privateKeyRef,
	).Scan(&out.ID, &out.Name, &out.SSHUser, &out.PublicKey, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return KeyPair{}, fmt.Errorf("create ssh key pair: %w", err)
	}

	return out, nil
}

func (s *Service) List() []KeyPair {
	rows, err := s.db.Query(
		`SELECT id, name, ssh_user, public_key, created_at, updated_at
		 FROM ssh_key_pairs
		 ORDER BY name ASC`,
	)
	if err != nil {
		return []KeyPair{}
	}
	defer rows.Close()

	out := make([]KeyPair, 0)
	for rows.Next() {
		var rec KeyPair
		if scanErr := rows.Scan(&rec.ID, &rec.Name, &rec.SSHUser, &rec.PublicKey, &rec.CreatedAt, &rec.UpdatedAt); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func (s *Service) GetByID(id int64) (KeyPair, error) {
	var rec KeyPair
	var privateKeyRef string
	err := s.db.QueryRow(
		`SELECT id, name, ssh_user, public_key, COALESCE(private_key, ''), COALESCE(private_key_secret_ref, ''), created_at, updated_at
		 FROM ssh_key_pairs
		 WHERE id = $1`,
		id,
	).Scan(&rec.ID, &rec.Name, &rec.SSHUser, &rec.PublicKey, &rec.PrivateKey, &privateKeyRef, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return KeyPair{}, ErrKeyPairNotFound
		}
		return KeyPair{}, fmt.Errorf("get ssh key pair: %w", err)
	}
	if strings.TrimSpace(privateKeyRef) != "" {
		value, err := s.secrets.Get(context.Background(), privateKeyRef)
		if err != nil {
			return KeyPair{}, fmt.Errorf("load ssh private key secret: %w", err)
		}
		rec.PrivateKey = string(value)
	}
	return rec, nil
}

func (s *Service) GetAutomationKey() (KeyPair, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM ssh_key_pairs
		ORDER BY is_automation_default DESC,
		         CASE WHEN lower(name) ~ '(terraform|ansible|automation)' THEN 0 ELSE 1 END,
		         id ASC
		LIMIT 1`).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return KeyPair{}, errors.New("no Terraform/Ansible automation SSH key pair is configured")
		}
		return KeyPair{}, fmt.Errorf("resolve automation ssh key pair: %w", err)
	}
	return s.GetByID(id)
}

func (s *Service) ListCustomerPublicKeys(scope tenant.Scope) ([]CustomerPublicKey, error) {
	query := `SELECT id, COALESCE(customer_account_id, 0), name, ssh_user, public_key, fingerprint, created_at, updated_at FROM customer_ssh_public_keys`
	args := []any{}
	if scope.IsCustomer() {
		query += ` WHERE customer_account_id = $1`
		args = append(args, scope.CustomerAccountID)
	} else {
		query += ` WHERE customer_account_id IS NULL`
	}
	query += ` ORDER BY name ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list customer ssh public keys: %w", err)
	}
	defer rows.Close()
	out := make([]CustomerPublicKey, 0)
	for rows.Next() {
		var rec CustomerPublicKey
		if err := rows.Scan(&rec.ID, &rec.CustomerAccountID, &rec.Name, &rec.SSHUser, &rec.PublicKey, &rec.Fingerprint, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan customer ssh public key: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) CreateCustomerPublicKey(scope tenant.Scope, in CustomerPublicKey) (CustomerPublicKey, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.SSHUser = strings.TrimSpace(in.SSHUser)
	in.PublicKey = strings.TrimSpace(in.PublicKey)
	if in.Name == "" {
		return CustomerPublicKey{}, errors.New("name is required")
	}
	if in.SSHUser == "" {
		in.SSHUser = "root"
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(in.PublicKey))
	if err != nil {
		return CustomerPublicKey{}, errors.New("public_key is not a valid OpenSSH authorized key")
	}
	in.Fingerprint = ssh.FingerprintSHA256(parsed)
	accountID := int64(0)
	if scope.IsCustomer() {
		accountID = scope.CustomerAccountID
	}
	err = s.db.QueryRow(
		`INSERT INTO customer_ssh_public_keys (customer_account_id, name, ssh_user, public_key, fingerprint, created_by_username)
		 VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6)
		 ON CONFLICT ((COALESCE(customer_account_id, 0)), fingerprint)
		 DO UPDATE SET name = EXCLUDED.name, ssh_user = EXCLUDED.ssh_user, public_key = EXCLUDED.public_key, updated_at = NOW()
		 RETURNING id, COALESCE(customer_account_id, 0), name, ssh_user, public_key, fingerprint, created_at, updated_at`,
		accountID, in.Name, in.SSHUser, in.PublicKey, in.Fingerprint, scope.Username,
	).Scan(&in.ID, &in.CustomerAccountID, &in.Name, &in.SSHUser, &in.PublicKey, &in.Fingerprint, &in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		return CustomerPublicKey{}, fmt.Errorf("save customer ssh public key: %w", err)
	}
	return in, nil
}

func (s *Service) GetCustomerPublicKey(id, customerAccountID int64) (CustomerPublicKey, error) {
	var rec CustomerPublicKey
	query := `SELECT id, COALESCE(customer_account_id, 0), name, ssh_user, public_key, fingerprint, created_at, updated_at FROM customer_ssh_public_keys WHERE id = $1`
	args := []any{id}
	if customerAccountID > 0 {
		query += ` AND customer_account_id = $2`
		args = append(args, customerAccountID)
	} else {
		query += ` AND customer_account_id IS NULL`
	}
	err := s.db.QueryRow(query, args...).Scan(&rec.ID, &rec.CustomerAccountID, &rec.Name, &rec.SSHUser, &rec.PublicKey, &rec.Fingerprint, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerPublicKey{}, errors.New("selected customer SSH public key was not found")
	}
	if err != nil {
		return CustomerPublicKey{}, fmt.Errorf("load customer ssh public key: %w", err)
	}
	return rec, nil
}

func (s *Service) DeleteCustomerPublicKey(scope tenant.Scope, id int64) error {
	query := `DELETE FROM customer_ssh_public_keys WHERE id = $1`
	args := []any{id}
	if scope.IsCustomer() {
		query += ` AND customer_account_id = $2`
		args = append(args, scope.CustomerAccountID)
	} else {
		query += ` AND customer_account_id IS NULL`
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("delete customer ssh public key: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return errors.New("customer SSH public key not found")
	}
	return nil
}

func (s *Service) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM ssh_key_pairs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete ssh key pair: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrKeyPairNotFound
	}
	return nil
}
