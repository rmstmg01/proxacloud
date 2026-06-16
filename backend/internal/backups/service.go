package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"proxacloud/backend/internal/secrets"
)

type Service struct {
	db      *sql.DB
	secrets secrets.Store
}

func NewService(db *sql.DB, secretStore secrets.Store) *Service {
	return &Service{db: db, secrets: secretStore}
}

func (s *Service) ListTargets() ([]Target, error) {
	rows, err := s.db.Query(
		`SELECT id, name, type, config_json, access_key_secret_ref, secret_key_secret_ref,
		        password_secret_ref, ssh_private_key_secret_ref, restic_password_secret_ref,
		        enabled, created_at, updated_at
		   FROM backup_targets
		  ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query backup targets: %w", err)
	}
	defer rows.Close()

	out := []Target{}
	for rows.Next() {
		var target Target
		var configBytes []byte
		if err := rows.Scan(
			&target.ID,
			&target.Name,
			&target.Type,
			&configBytes,
			&target.AccessKeySecretRef,
			&target.SecretKeySecretRef,
			&target.PasswordSecretRef,
			&target.SSHPrivateKeySecretRef,
			&target.ResticPasswordRef,
			&target.Enabled,
			&target.CreatedAt,
			&target.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan backup target: %w", err)
		}
		target.Config = map[string]any{}
		if len(configBytes) > 0 {
			_ = json.Unmarshal(configBytes, &target.Config)
		}
		target.HasAccessKey = target.AccessKeySecretRef != ""
		target.HasSecretKey = target.SecretKeySecretRef != ""
		target.HasPassword = target.PasswordSecretRef != ""
		target.HasSSHPrivateKey = target.SSHPrivateKeySecretRef != ""
		target.HasResticPassword = target.ResticPasswordRef != ""
		out = append(out, target)
	}
	return out, nil
}

func (s *Service) Summary() (Summary, error) {
	targets, err := s.ListTargets()
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Targets: targets, TargetCount: len(targets)}
	for _, target := range targets {
		if target.Enabled {
			summary.EnabledCount++
		}
		if target.Type == TargetS3 || target.Type == TargetHetzner {
			summary.ExternalCount++
		}
	}
	return summary, nil
}

func (s *Service) CreateTarget(in TargetInput) (Target, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Target{}, errors.New("name is required")
	}
	if err := validateTargetType(in.Type); err != nil {
		return Target{}, err
	}
	config := sanitizeConfig(in.Type, in.Config)
	if err := validateConfig(in.Type, config); err != nil {
		return Target{}, err
	}
	if err := validateSecrets(in.Type, config, in); err != nil {
		return Target{}, err
	}
	if !in.EnabledSet {
		in.Enabled = true
	}

	ctx := context.Background()
	scope := "backup-targets/" + in.Name
	accessRef, err := putOptionalSecret(s.secrets, ctx, scope, "access-key", in.AccessKey)
	if err != nil {
		return Target{}, err
	}
	secretRef, err := putOptionalSecret(s.secrets, ctx, scope, "secret-key", in.SecretKey)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef)
		return Target{}, err
	}
	passwordRef, err := putOptionalSecret(s.secrets, ctx, scope, "password", in.Password)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef, secretRef)
		return Target{}, err
	}
	sshKeyRef, err := putOptionalSecret(s.secrets, ctx, scope, "ssh-private-key", in.SSHPrivateKey)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef, secretRef, passwordRef)
		return Target{}, err
	}
	resticRef, err := putOptionalSecret(s.secrets, ctx, scope, "restic-password", in.ResticPassword)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef, secretRef, passwordRef, sshKeyRef)
		return Target{}, err
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef, secretRef, passwordRef, sshKeyRef, resticRef)
		return Target{}, fmt.Errorf("encode target config: %w", err)
	}

	var target Target
	var storedConfig []byte
	err = s.db.QueryRow(
		`INSERT INTO backup_targets
		 (name, type, config_json, access_key_secret_ref, secret_key_secret_ref,
		  password_secret_ref, ssh_private_key_secret_ref, restic_password_secret_ref, enabled)
		 VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $9)
		 RETURNING id, name, type, config_json, access_key_secret_ref, secret_key_secret_ref,
		           password_secret_ref, ssh_private_key_secret_ref, restic_password_secret_ref,
		           enabled, created_at, updated_at`,
		in.Name,
		string(in.Type),
		string(configBytes),
		accessRef,
		secretRef,
		passwordRef,
		sshKeyRef,
		resticRef,
		in.Enabled,
	).Scan(
		&target.ID,
		&target.Name,
		&target.Type,
		&storedConfig,
		&target.AccessKeySecretRef,
		&target.SecretKeySecretRef,
		&target.PasswordSecretRef,
		&target.SSHPrivateKeySecretRef,
		&target.ResticPasswordRef,
		&target.Enabled,
		&target.CreatedAt,
		&target.UpdatedAt,
	)
	if err != nil {
		cleanupSecrets(s.secrets, ctx, accessRef, secretRef, passwordRef, sshKeyRef, resticRef)
		return Target{}, fmt.Errorf("insert backup target: %w", err)
	}
	target.Config = map[string]any{}
	_ = json.Unmarshal(storedConfig, &target.Config)
	target.HasAccessKey = target.AccessKeySecretRef != ""
	target.HasSecretKey = target.SecretKeySecretRef != ""
	target.HasPassword = target.PasswordSecretRef != ""
	target.HasSSHPrivateKey = target.SSHPrivateKeySecretRef != ""
	target.HasResticPassword = target.ResticPasswordRef != ""
	return target, nil
}

func (s *Service) DeleteTarget(id int64) error {
	target, err := s.getTarget(id)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM backup_targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete backup target: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("backup target not found")
	}
	cleanupSecrets(s.secrets, context.Background(),
		target.AccessKeySecretRef,
		target.SecretKeySecretRef,
		target.PasswordSecretRef,
		target.SSHPrivateKeySecretRef,
		target.ResticPasswordRef,
	)
	return nil
}

func (s *Service) getTarget(id int64) (Target, error) {
	targets, err := s.ListTargets()
	if err != nil {
		return Target{}, err
	}
	for _, target := range targets {
		if target.ID == id {
			return target, nil
		}
	}
	return Target{}, errors.New("backup target not found")
}

func validateTargetType(t TargetType) error {
	switch t {
	case TargetProxmoxStorage, TargetPBS, TargetS3, TargetHetzner:
		return nil
	default:
		return fmt.Errorf("unsupported backup target type %q", t)
	}
}

func sanitizeConfig(t TargetType, config map[string]any) map[string]any {
	out := map[string]any{}
	allowed := map[string]bool{}
	switch t {
	case TargetS3:
		allowed = map[string]bool{
			"bucket": true, "region": true, "endpoint_url": true, "prefix": true,
			"storage_class": true, "server_side_encryption": true, "path_style": true,
		}
	case TargetHetzner:
		allowed = map[string]bool{
			"host": true, "port": true, "username": true, "base_path": true,
			"auth_method": true, "backup_mode": true,
		}
	case TargetProxmoxStorage:
		allowed = map[string]bool{"storage_id": true, "node_name": true}
	case TargetPBS:
		allowed = map[string]bool{"datastore": true, "namespace": true, "repository": true}
	}
	for key, value := range config {
		if allowed[key] {
			out[key] = value
		}
	}
	return out
}

func validateConfig(t TargetType, config map[string]any) error {
	required := []string{}
	switch t {
	case TargetS3:
		required = []string{"bucket"}
	case TargetHetzner:
		required = []string{"host", "username", "base_path"}
	case TargetProxmoxStorage:
		required = []string{"storage_id"}
	case TargetPBS:
		required = []string{"datastore"}
	}
	for _, key := range required {
		value, _ := config[key].(string)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func validateSecrets(t TargetType, config map[string]any, in TargetInput) error {
	if t != TargetHetzner {
		return nil
	}
	authMethod, _ := config["auth_method"].(string)
	switch strings.TrimSpace(authMethod) {
	case "ssh_key":
		if strings.TrimSpace(in.SSHPrivateKey) == "" {
			return errors.New("ssh_private_key is required when auth_method is ssh_key")
		}
	case "", "password":
		if strings.TrimSpace(in.Password) == "" {
			return errors.New("password is required when auth_method is password")
		}
	default:
		return fmt.Errorf("unsupported auth_method %q", authMethod)
	}
	return nil
}

func putOptionalSecret(store secrets.Store, ctx context.Context, scope, name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	return store.Put(ctx, scope, name, []byte(value))
}

func cleanupSecrets(store secrets.Store, ctx context.Context, refs ...string) {
	for _, ref := range refs {
		if strings.TrimSpace(ref) != "" {
			_ = store.Delete(ctx, ref)
		}
	}
}
