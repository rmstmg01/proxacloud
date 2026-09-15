package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrSecretNotFound = errors.New("secret not found")

type Store interface {
	Put(ctx context.Context, scope, name string, value []byte) (string, error)
	Get(ctx context.Context, ref string) ([]byte, error)
	Delete(ctx context.Context, ref string) error
}

type DBStore struct {
	db   *sql.DB
	aead cipher.AEAD
}

func NewDBStore(db *sql.DB, keyMaterial string) (*DBStore, error) {
	key := normalizeKey(keyMaterial)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("initialize secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize secret aead: %w", err)
	}
	return &DBStore{db: db, aead: aead}, nil
}

func (s *DBStore) Put(ctx context.Context, scope, name string, value []byte) (string, error) {
	return s.put(ctx, s.db, scope, name, value)
}

// PutTx keeps encrypted payload storage atomic with the caller's state change.
func (s *DBStore) PutTx(ctx context.Context, tx *sql.Tx, scope, name string, value []byte) (string, error) {
	return s.put(ctx, tx, scope, name, value)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *DBStore) put(ctx context.Context, db queryRower, scope, name string, value []byte) (string, error) {
	scope = strings.TrimSpace(scope)
	name = strings.TrimSpace(name)
	if scope == "" {
		return "", errors.New("secret scope is required")
	}
	if name == "" {
		return "", errors.New("secret name is required")
	}
	if len(value) == 0 {
		return "", errors.New("secret value is required")
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nil, nonce, value, additionalData(scope, name))

	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO managed_secrets (scope, name, nonce, ciphertext)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		scope,
		name,
		nonce,
		ciphertext,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store secret: %w", err)
	}
	return fmt.Sprintf("db:%d", id), nil
}

func (s *DBStore) Get(ctx context.Context, ref string) ([]byte, error) {
	id, err := parseDBRef(ref)
	if err != nil {
		return nil, err
	}

	var scope, name string
	var nonce, ciphertext []byte
	err = s.db.QueryRowContext(ctx,
		`SELECT scope, name, nonce, ciphertext
		 FROM managed_secrets
		 WHERE id = $1`,
		id,
	).Scan(&scope, &name, &nonce, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load secret: %w", err)
	}

	plaintext, err := s.aead.Open(nil, nonce, ciphertext, additionalData(scope, name))
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}

func (s *DBStore) Delete(ctx context.Context, ref string) error {
	id, err := parseDBRef(ref)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM managed_secrets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}

func additionalData(scope, name string) []byte {
	return []byte(scope + "\x00" + name)
}

func parseDBRef(ref string) (int64, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "db:") {
		return 0, fmt.Errorf("unsupported secret ref %q", ref)
	}
	var id int64
	if _, err := fmt.Sscanf(ref, "db:%d", &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid secret ref %q", ref)
	}
	return id, nil
}

func normalizeKey(raw string) [32]byte {
	raw = strings.TrimSpace(raw)
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return [32]byte(decoded)
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
		return [32]byte(decoded)
	}
	return sha256.Sum256([]byte(raw))
}
