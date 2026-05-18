package sslcerts

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrCertificateNotFound = errors.New("ssl certificate not found")

type Certificate struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullchainPEM  string    `json:"fullchain_pem,omitempty"`
	PrivateKeyPEM string    `json:"private_key_pem,omitempty"`
	Subject       string    `json:"subject,omitempty"`
	DNSNames      []string  `json:"dns_names"`
	NotAfter      time.Time `json:"not_after,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(in Certificate) (Certificate, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.FullchainPEM = strings.TrimSpace(in.FullchainPEM)
	in.PrivateKeyPEM = strings.TrimSpace(in.PrivateKeyPEM)

	if in.Name == "" {
		return Certificate{}, errors.New("name is required")
	}
	if in.FullchainPEM == "" {
		return Certificate{}, errors.New("fullchain_pem is required")
	}
	if in.PrivateKeyPEM == "" {
		return Certificate{}, errors.New("private_key_pem is required")
	}
	if _, err := tls.X509KeyPair([]byte(in.FullchainPEM), []byte(in.PrivateKeyPEM)); err != nil {
		return Certificate{}, fmt.Errorf("invalid TLS certificate/key pair: %w", err)
	}

	var out Certificate
	err := s.db.QueryRow(
		`INSERT INTO ssl_certificates (name, fullchain_pem, private_key_pem)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, fullchain_pem, created_at, updated_at`,
		in.Name,
		in.FullchainPEM,
		in.PrivateKeyPEM,
	).Scan(&out.ID, &out.Name, &out.FullchainPEM, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Certificate{}, fmt.Errorf("create ssl certificate: %w", err)
	}
	enrichCertificateSummary(&out)
	out.FullchainPEM = ""
	return out, nil
}

func (s *Service) List() []Certificate {
	rows, err := s.db.Query(
		`SELECT id, name, fullchain_pem, created_at, updated_at
		 FROM ssl_certificates
		 ORDER BY name ASC`,
	)
	if err != nil {
		return []Certificate{}
	}
	defer rows.Close()

	out := make([]Certificate, 0)
	for rows.Next() {
		var rec Certificate
		if scanErr := rows.Scan(&rec.ID, &rec.Name, &rec.FullchainPEM, &rec.CreatedAt, &rec.UpdatedAt); scanErr != nil {
			continue
		}
		enrichCertificateSummary(&rec)
		rec.FullchainPEM = ""
		out = append(out, rec)
	}
	return out
}

func (s *Service) GetByID(id int64) (Certificate, error) {
	var rec Certificate
	err := s.db.QueryRow(
		`SELECT id, name, fullchain_pem, private_key_pem, created_at, updated_at
		 FROM ssl_certificates
		 WHERE id = $1`,
		id,
	).Scan(&rec.ID, &rec.Name, &rec.FullchainPEM, &rec.PrivateKeyPEM, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Certificate{}, ErrCertificateNotFound
		}
		return Certificate{}, fmt.Errorf("get ssl certificate: %w", err)
	}
	enrichCertificateSummary(&rec)
	return rec, nil
}

func (s *Service) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM ssl_certificates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete ssl certificate: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrCertificateNotFound
	}
	return nil
}

func enrichCertificateSummary(cert *Certificate) {
	block, _ := pem.Decode([]byte(cert.FullchainPEM))
	if block == nil {
		return
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}
	cert.Subject = parsed.Subject.CommonName
	cert.DNSNames = parsed.DNSNames
	cert.NotAfter = parsed.NotAfter
}
