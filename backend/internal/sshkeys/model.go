package sshkeys

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
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
		in.SSHUser = "ubuntu"
	}
	if in.PublicKey == "" {
		return KeyPair{}, errors.New("public_key is required")
	}
	if in.PrivateKey == "" {
		return KeyPair{}, errors.New("private_key is required")
	}

	var out KeyPair
	err := s.db.QueryRow(
		`INSERT INTO ssh_key_pairs (name, ssh_user, public_key, private_key)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, ssh_user, public_key, created_at, updated_at`,
		in.Name,
		in.SSHUser,
		in.PublicKey,
		in.PrivateKey,
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
	err := s.db.QueryRow(
		`SELECT id, name, ssh_user, public_key, private_key, created_at, updated_at
		 FROM ssh_key_pairs
		 WHERE id = $1`,
		id,
	).Scan(&rec.ID, &rec.Name, &rec.SSHUser, &rec.PublicKey, &rec.PrivateKey, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return KeyPair{}, ErrKeyPairNotFound
		}
		return KeyPair{}, fmt.Errorf("get ssh key pair: %w", err)
	}
	return rec, nil
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
