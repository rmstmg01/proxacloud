package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"proxacloud/backend/internal/proxmox"
	"proxacloud/backend/internal/secrets"
)

type Service struct {
	db      *sql.DB
	secrets secrets.Store
	proxmox ProxmoxBackupper
}

type ProxmoxBackupper interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	BackupVMByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, storageID string, mode string) (string, error)
	BackupContainerByName(cred proxmox.Credential, proxmoxNodeName string, containerName string, storageID string, mode string) (string, error)
}

func NewService(db *sql.DB, secretStore secrets.Store, proxmoxBackupper ProxmoxBackupper) *Service {
	return &Service{db: db, secrets: secretStore, proxmox: proxmoxBackupper}
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
	jobs, err := s.ListJobs()
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Targets: targets, Jobs: jobs, TargetCount: len(targets)}
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

func (s *Service) ListJobs() ([]Job, error) {
	rows, err := s.db.Query(
		`SELECT j.id, j.target_id, COALESCE(t.name, ''), j.source_type, j.source_id, j.source_name,
		        j.mode, j.status, j.proxmox_node_name, j.proxmox_task_id, j.output, j.error, j.created_at, j.updated_at
		   FROM backup_jobs j
		   LEFT JOIN backup_targets t ON t.id = j.target_id
		  ORDER BY j.created_at DESC
		  LIMIT 50`,
	)
	if err != nil {
		return nil, fmt.Errorf("query backup jobs: %w", err)
	}
	defer rows.Close()

	out := []Job{}
	for rows.Next() {
		var job Job
		if err := rows.Scan(
			&job.ID,
			&job.TargetID,
			&job.TargetName,
			&job.SourceType,
			&job.SourceID,
			&job.SourceName,
			&job.Mode,
			&job.Status,
			&job.ProxmoxNodeName,
			&job.ProxmoxTaskID,
			&job.Output,
			&job.Error,
			&job.CreatedAt,
			&job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan backup job: %w", err)
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Service) StartJob(in JobInput) (Job, error) {
	if s.proxmox == nil {
		return Job{}, errors.New("proxmox backup runner is not configured")
	}
	if in.TargetID <= 0 {
		return Job{}, errors.New("target_id is required")
	}
	if in.SourceID <= 0 {
		return Job{}, errors.New("source_id is required")
	}
	mode := strings.TrimSpace(strings.ToLower(in.Mode))
	if mode == "" {
		mode = "snapshot"
	}
	if mode != "snapshot" && mode != "suspend" && mode != "stop" {
		return Job{}, fmt.Errorf("unsupported backup mode %q", mode)
	}

	target, err := s.getTarget(in.TargetID)
	if err != nil {
		return Job{}, err
	}
	if !target.Enabled {
		return Job{}, errors.New("backup target is disabled")
	}
	storageID := backupStorageID(target, in.StagingStorageID)
	if storageID == "" {
		if target.Type == TargetProxmoxStorage {
			return Job{}, errors.New("target storage_id is required")
		}
		return Job{}, errors.New("staging_storage_id is required for external backup targets")
	}

	source, err := s.resolveBackupSource(in.SourceType, in.SourceID)
	if err != nil {
		return Job{}, err
	}
	job, err := s.insertJob(target.ID, in.SourceType, in.SourceID, source.name, mode, source.nodeName)
	if err != nil {
		return Job{}, err
	}

	cred, err := s.proxmox.GetCredentialWithSecrets(source.credentialID)
	if err != nil {
		_ = s.finishJob(job.ID, "failed", "", "", err.Error())
		return s.getJob(job.ID)
	}

	var taskID string
	switch in.SourceType {
	case SourceVPS:
		taskID, err = s.proxmox.BackupVMByName(cred, source.nodeName, source.proxmoxName, storageID, mode)
	case SourceContainer:
		taskID, err = s.proxmox.BackupContainerByName(cred, source.nodeName, source.proxmoxName, storageID, mode)
	case SourceCluster:
		err = errors.New("full cluster backup is not implemented yet; use etcd snapshot for cluster state and back up node VMs individually")
	default:
		err = fmt.Errorf("unsupported backup source_type %q", in.SourceType)
	}
	if err != nil {
		_ = s.finishJob(job.ID, "failed", "", "", err.Error())
		return s.getJob(job.ID)
	}
	output := fmt.Sprintf("Proxmox vzdump started for %s %s on node %s using storage %s", in.SourceType, source.name, source.nodeName, storageID)
	if target.Type != TargetProxmoxStorage {
		output += fmt.Sprintf("; external upload to %s target %q is pending the upload runner phase", target.Type, target.Name)
	}
	if err := s.finishJob(job.ID, "running", taskID, output, ""); err != nil {
		return Job{}, err
	}
	return s.getJob(job.ID)
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

func backupStorageID(target Target, stagingStorageID string) string {
	if target.Type != TargetProxmoxStorage {
		return strings.TrimSpace(stagingStorageID)
	}
	storageID, _ := target.Config["storage_id"].(string)
	return strings.TrimSpace(storageID)
}

type backupSource struct {
	name         string
	proxmoxName  string
	nodeName     string
	credentialID int64
}

func (s *Service) resolveBackupSource(sourceType SourceType, sourceID int64) (backupSource, error) {
	switch sourceType {
	case SourceVPS:
		var src backupSource
		err := s.db.QueryRow(
			`SELECT name, vm_name, proxmox_node_name, proxmox_credential_id
			   FROM vps_instances
			  WHERE id = $1`,
			sourceID,
		).Scan(&src.name, &src.proxmoxName, &src.nodeName, &src.credentialID)
		if errors.Is(err, sql.ErrNoRows) {
			return backupSource{}, errors.New("vps source not found")
		}
		if err != nil {
			return backupSource{}, fmt.Errorf("query vps source: %w", err)
		}
		return src, nil
	case SourceContainer:
		var src backupSource
		err := s.db.QueryRow(
			`SELECT name, container_name, proxmox_node_name, proxmox_credential_id
			   FROM container_instances
			  WHERE id = $1`,
			sourceID,
		).Scan(&src.name, &src.proxmoxName, &src.nodeName, &src.credentialID)
		if errors.Is(err, sql.ErrNoRows) {
			return backupSource{}, errors.New("container source not found")
		}
		if err != nil {
			return backupSource{}, fmt.Errorf("query container source: %w", err)
		}
		return src, nil
	case SourceCluster:
		var src backupSource
		err := s.db.QueryRow(
			`SELECT name, '', COALESCE(proxmox_node_name, ''), proxmox_credential_id
			   FROM clusters
			  WHERE id = $1`,
			sourceID,
		).Scan(&src.name, &src.proxmoxName, &src.nodeName, &src.credentialID)
		if errors.Is(err, sql.ErrNoRows) {
			return backupSource{}, errors.New("cluster source not found")
		}
		if err != nil {
			return backupSource{}, fmt.Errorf("query cluster source: %w", err)
		}
		return src, nil
	default:
		return backupSource{}, fmt.Errorf("unsupported backup source_type %q", sourceType)
	}
}

func (s *Service) insertJob(targetID int64, sourceType SourceType, sourceID int64, sourceName string, mode string, nodeName string) (Job, error) {
	var job Job
	err := s.db.QueryRow(
		`INSERT INTO backup_jobs (target_id, source_type, source_id, source_name, mode, status, proxmox_node_name)
		 VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		 RETURNING id, target_id, source_type, source_id, source_name, mode, status, proxmox_node_name, proxmox_task_id, output, error, created_at, updated_at`,
		targetID,
		string(sourceType),
		sourceID,
		strings.TrimSpace(sourceName),
		mode,
		strings.TrimSpace(nodeName),
	).Scan(&job.ID, &job.TargetID, &job.SourceType, &job.SourceID, &job.SourceName, &job.Mode, &job.Status, &job.ProxmoxNodeName, &job.ProxmoxTaskID, &job.Output, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return Job{}, fmt.Errorf("insert backup job: %w", err)
	}
	return job, nil
}

func (s *Service) finishJob(jobID int64, status string, taskID string, output string, errText string) error {
	_, err := s.db.Exec(
		`UPDATE backup_jobs
		    SET status = $2, proxmox_task_id = $3, output = $4, error = $5, updated_at = NOW()
		  WHERE id = $1`,
		jobID,
		status,
		strings.TrimSpace(taskID),
		output,
		errText,
	)
	return err
}

func (s *Service) getJob(id int64) (Job, error) {
	var job Job
	err := s.db.QueryRow(
		`SELECT j.id, j.target_id, COALESCE(t.name, ''), j.source_type, j.source_id, j.source_name,
		        j.mode, j.status, j.proxmox_node_name, j.proxmox_task_id, j.output, j.error, j.created_at, j.updated_at
		   FROM backup_jobs j
		   LEFT JOIN backup_targets t ON t.id = j.target_id
		  WHERE j.id = $1`,
		id,
	).Scan(&job.ID, &job.TargetID, &job.TargetName, &job.SourceType, &job.SourceID, &job.SourceName, &job.Mode, &job.Status, &job.ProxmoxNodeName, &job.ProxmoxTaskID, &job.Output, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, errors.New("backup job not found")
	}
	if err != nil {
		return Job{}, fmt.Errorf("query backup job: %w", err)
	}
	return job, nil
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
