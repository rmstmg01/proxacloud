package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	GetTaskStatus(cred proxmox.Credential, proxmoxNodeName string, upid string) (proxmox.TaskStatus, error)
	ListStorageContent(cred proxmox.Credential, proxmoxNodeName string, storageID string, content string) ([]proxmox.StorageContent, error)
	DownloadStorageContent(cred proxmox.Credential, proxmoxNodeName string, storageID string, volID string, destinationPath string) error
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
		        j.mode, j.status, j.proxmox_node_name, j.proxmox_task_id, j.staging_storage_id,
		        j.artifact_volid, j.artifact_path, j.upload_method, j.upload_status, j.remote_path,
		        j.upload_output, j.upload_error, j.output, j.error, j.created_at, j.updated_at
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
			&job.StagingStorageID,
			&job.ArtifactVolID,
			&job.ArtifactPath,
			&job.UploadMethod,
			&job.UploadStatus,
			&job.RemotePath,
			&job.UploadOutput,
			&job.UploadError,
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
	uploadMethod := backupUploadMethod(target)
	job, err := s.insertJob(target.ID, in.SourceType, in.SourceID, source.name, mode, source.nodeName, storageID, uploadMethod)
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
	if target.Type == TargetHetzner {
		output += fmt.Sprintf("; %s upload to %s target %q will start after vzdump completes", uploadMethod, target.Type, target.Name)
	} else if target.Type != TargetProxmoxStorage {
		output += fmt.Sprintf("; external upload to %s target %q is pending an upload runner", target.Type, target.Name)
	}
	if err := s.finishJob(job.ID, "running", taskID, output, ""); err != nil {
		return Job{}, err
	}
	if target.Type == TargetHetzner {
		go s.runExternalUpload(job.ID, target.ID, source.credentialID)
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

func (s *Service) insertJob(targetID int64, sourceType SourceType, sourceID int64, sourceName string, mode string, nodeName string, stagingStorageID string, uploadMethod string) (Job, error) {
	var job Job
	uploadStatus := "not_required"
	if strings.TrimSpace(uploadMethod) != "" {
		uploadStatus = "pending"
	}
	err := s.db.QueryRow(
		`INSERT INTO backup_jobs
		 (target_id, source_type, source_id, source_name, mode, status, proxmox_node_name, staging_storage_id, upload_method, upload_status)
		 VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8, $9)
		 RETURNING id, target_id, source_type, source_id, source_name, mode, status, proxmox_node_name,
		           proxmox_task_id, staging_storage_id, artifact_volid, artifact_path, upload_method,
		           upload_status, remote_path, upload_output, upload_error, output, error, created_at, updated_at`,
		targetID,
		string(sourceType),
		sourceID,
		strings.TrimSpace(sourceName),
		mode,
		strings.TrimSpace(nodeName),
		strings.TrimSpace(stagingStorageID),
		strings.TrimSpace(uploadMethod),
		uploadStatus,
	).Scan(&job.ID, &job.TargetID, &job.SourceType, &job.SourceID, &job.SourceName, &job.Mode, &job.Status, &job.ProxmoxNodeName, &job.ProxmoxTaskID, &job.StagingStorageID, &job.ArtifactVolID, &job.ArtifactPath, &job.UploadMethod, &job.UploadStatus, &job.RemotePath, &job.UploadOutput, &job.UploadError, &job.Output, &job.Error, &job.CreatedAt, &job.UpdatedAt)
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
		        j.mode, j.status, j.proxmox_node_name, j.proxmox_task_id, j.staging_storage_id,
		        j.artifact_volid, j.artifact_path, j.upload_method, j.upload_status, j.remote_path,
		        j.upload_output, j.upload_error, j.output, j.error, j.created_at, j.updated_at
		   FROM backup_jobs j
		   LEFT JOIN backup_targets t ON t.id = j.target_id
		  WHERE j.id = $1`,
		id,
	).Scan(&job.ID, &job.TargetID, &job.TargetName, &job.SourceType, &job.SourceID, &job.SourceName, &job.Mode, &job.Status, &job.ProxmoxNodeName, &job.ProxmoxTaskID, &job.StagingStorageID, &job.ArtifactVolID, &job.ArtifactPath, &job.UploadMethod, &job.UploadStatus, &job.RemotePath, &job.UploadOutput, &job.UploadError, &job.Output, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, errors.New("backup job not found")
	}
	if err != nil {
		return Job{}, fmt.Errorf("query backup job: %w", err)
	}
	return job, nil
}

func (s *Service) runExternalUpload(jobID int64, targetID int64, credentialID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()

	job, err := s.getJob(jobID)
	if err != nil {
		_ = s.markUploadFailed(jobID, "load backup job", err)
		return
	}
	target, err := s.getTarget(targetID)
	if err != nil {
		_ = s.markUploadFailed(jobID, "load backup target", err)
		return
	}
	cred, err := s.proxmox.GetCredentialWithSecrets(credentialID)
	if err != nil {
		_ = s.markUploadFailed(jobID, "load proxmox credential", err)
		return
	}

	taskStatus, err := s.waitForBackupTask(ctx, cred, job)
	if err != nil {
		_ = s.markUploadFailed(jobID, "wait for proxmox backup task", err)
		return
	}
	if strings.TrimSpace(taskStatus.ExitStatus) != "OK" {
		_ = s.finishJob(jobID, "failed", job.ProxmoxTaskID, job.Output, "proxmox backup task finished with exit status "+taskStatus.ExitStatus)
		_ = s.updateUpload(jobID, "failed", "", "", "proxmox backup task failed: "+taskStatus.ExitStatus, "")
		return
	}

	vmid, err := vmIDFromUPID(job.ProxmoxTaskID)
	if err != nil {
		_ = s.markUploadFailed(jobID, "parse proxmox task id", err)
		return
	}
	artifact, err := s.findBackupArtifact(cred, job, vmid)
	if err != nil {
		_ = s.markUploadFailed(jobID, "find proxmox backup artifact", err)
		return
	}

	workDir := filepath.Join("backups", "runs", fmt.Sprintf("job-%d", jobID))
	if err := os.MkdirAll(workDir, 0700); err != nil {
		_ = s.markUploadFailed(jobID, "prepare backup work directory", err)
		return
	}
	localPath := filepath.Join(workDir, storageFilename(artifact.VolID))
	if err := s.proxmox.DownloadStorageContent(cred, job.ProxmoxNodeName, job.StagingStorageID, artifact.VolID, localPath); err != nil {
		_ = s.markUploadFailed(jobID, "download proxmox backup artifact", err)
		return
	}
	_ = s.updateArtifact(jobID, artifact.VolID, localPath)

	method := backupUploadMethod(target)
	_ = s.updateUpload(jobID, "running", "", "", "", "")
	var remotePath, output string
	switch method {
	case "rsync":
		remotePath, output, err = s.uploadWithRsync(ctx, target, localPath)
	default:
		remotePath, output, err = s.uploadWithRestic(ctx, target, localPath, job)
	}
	if err != nil {
		_ = s.updateUpload(jobID, "failed", remotePath, output, err.Error(), method)
		_ = s.finishJob(jobID, "failed", job.ProxmoxTaskID, job.Output, err.Error())
		return
	}
	output = truncateOutput(output)
	_ = s.updateUpload(jobID, "completed", remotePath, output, "", method)
	_ = s.finishJob(jobID, "completed", job.ProxmoxTaskID, job.Output+"; external upload completed", "")
}

func (s *Service) waitForBackupTask(ctx context.Context, cred proxmox.Credential, job Job) (proxmox.TaskStatus, error) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		status, err := s.proxmox.GetTaskStatus(cred, job.ProxmoxNodeName, job.ProxmoxTaskID)
		if err != nil {
			return proxmox.TaskStatus{}, err
		}
		if strings.EqualFold(status.Status, "stopped") {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return proxmox.TaskStatus{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) findBackupArtifact(cred proxmox.Credential, job Job, vmid string) (proxmox.StorageContent, error) {
	items, err := s.proxmox.ListStorageContent(cred, job.ProxmoxNodeName, job.StagingStorageID, "backup")
	if err != nil {
		return proxmox.StorageContent{}, err
	}
	prefix := "vzdump-qemu-" + vmid + "-"
	if job.SourceType == SourceContainer {
		prefix = "vzdump-lxc-" + vmid + "-"
	}
	matches := make([]proxmox.StorageContent, 0)
	for _, item := range items {
		name := storageFilename(item.VolID)
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return proxmox.StorageContent{}, fmt.Errorf("no backup artifact matching %s found in storage %s", prefix, job.StagingStorageID)
	}
	sort.Slice(matches, func(i, j int) bool {
		return storageFilename(matches[i].VolID) > storageFilename(matches[j].VolID)
	})
	return matches[0], nil
}

func (s *Service) uploadWithRestic(ctx context.Context, target Target, localPath string, job Job) (string, string, error) {
	if _, err := exec.LookPath("restic"); err != nil {
		return "", "", errors.New("restic binary is not installed in the backend runtime")
	}
	auth, err := s.hetznerAuth(ctx, target)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(auth.resticPassword) == "" {
		return "", "", errors.New("restic_password is required for restic backup targets")
	}
	cleanup, err := auth.prepareSSHCommand()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	repo := fmt.Sprintf("sftp:%s@%s:%s", auth.username, auth.host, auth.basePath)
	env := append(os.Environ(), "RESTIC_REPOSITORY="+repo, "RESTIC_PASSWORD="+auth.resticPassword)
	if auth.password != "" {
		env = append(env, "SSHPASS="+auth.password)
	}
	args := []string{}
	if auth.sftpCommand != "" {
		args = append(args, "-o", "sftp.command="+auth.sftpCommand)
	}

	var combined strings.Builder
	if out, err := runCommand(ctx, env, "restic", append(args, "snapshots")...); err != nil {
		initOut, initErr := runCommand(ctx, env, "restic", append(args, "init")...)
		combined.WriteString(initOut)
		if initErr != nil {
			return repo, combined.String(), fmt.Errorf("initialize restic repository: %w", initErr)
		}
	} else {
		combined.WriteString(out)
	}

	backupArgs := append(args,
		"backup", localPath,
		"--host", strings.TrimSpace(job.ProxmoxNodeName),
		"--tag", "proxacloud",
		"--tag", string(job.SourceType),
		"--tag", fmt.Sprintf("source-%d", job.SourceID),
	)
	out, err := runCommand(ctx, env, "restic", backupArgs...)
	combined.WriteString(out)
	if err != nil {
		return repo, combined.String(), fmt.Errorf("run restic backup: %w", err)
	}
	return repo, combined.String(), nil
}

func (s *Service) uploadWithRsync(ctx context.Context, target Target, localPath string) (string, string, error) {
	if _, err := exec.LookPath("rsync"); err != nil {
		return "", "", errors.New("rsync binary is not installed in the backend runtime")
	}
	auth, err := s.hetznerAuth(ctx, target)
	if err != nil {
		return "", "", err
	}
	cleanup, err := auth.prepareSSHCommand()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	remote := fmt.Sprintf("%s@%s:%s/", auth.username, auth.host, strings.TrimRight(auth.basePath, "/"))
	env := os.Environ()
	if auth.password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return remote, "", errors.New("sshpass binary is required for rsync password authentication")
		}
		env = append(env, "SSHPASS="+auth.password)
		out, err := runCommand(ctx, env, "sshpass", "-e", "rsync", "-av", "--partial", "-e", auth.rsyncSSHCommand(), localPath, remote)
		if err != nil {
			return remote, out, fmt.Errorf("run rsync upload: %w", err)
		}
		return remote, out, nil
	}
	out, err := runCommand(ctx, env, "rsync", "-av", "--partial", "-e", auth.rsyncSSHCommand(), localPath, remote)
	if err != nil {
		return remote, out, fmt.Errorf("run rsync upload: %w", err)
	}
	return remote, out, nil
}

type hetznerAuth struct {
	host           string
	port           string
	username       string
	basePath       string
	password       string
	sshPrivateKey  string
	resticPassword string
	sftpCommand    string
	keyPath        string
}

func (s *Service) hetznerAuth(ctx context.Context, target Target) (hetznerAuth, error) {
	value := func(key string) string {
		switch raw := target.Config[key].(type) {
		case string:
			return strings.TrimSpace(raw)
		case float64:
			return strings.TrimSpace(fmt.Sprintf("%.0f", raw))
		case int:
			return strings.TrimSpace(fmt.Sprintf("%d", raw))
		default:
			return ""
		}
	}
	auth := hetznerAuth{
		host:     value("host"),
		port:     value("port"),
		username: value("username"),
		basePath: value("base_path"),
	}
	if auth.port == "" {
		auth.port = "23"
	}
	if auth.host == "" || auth.username == "" || auth.basePath == "" {
		return hetznerAuth{}, errors.New("hetzner target host, username, and base_path are required")
	}
	if target.PasswordSecretRef != "" {
		secret, err := s.secrets.Get(ctx, target.PasswordSecretRef)
		if err != nil {
			return hetznerAuth{}, fmt.Errorf("load hetzner password: %w", err)
		}
		auth.password = strings.TrimSpace(string(secret))
	}
	if target.SSHPrivateKeySecretRef != "" {
		secret, err := s.secrets.Get(ctx, target.SSHPrivateKeySecretRef)
		if err != nil {
			return hetznerAuth{}, fmt.Errorf("load hetzner ssh private key: %w", err)
		}
		auth.sshPrivateKey = strings.TrimSpace(string(secret))
	}
	if target.ResticPasswordRef != "" {
		secret, err := s.secrets.Get(ctx, target.ResticPasswordRef)
		if err != nil {
			return hetznerAuth{}, fmt.Errorf("load restic password: %w", err)
		}
		auth.resticPassword = strings.TrimSpace(string(secret))
	}
	return auth, nil
}

func (a *hetznerAuth) prepareSSHCommand() (func(), error) {
	cleanup := func() {}
	common := "-p " + a.port + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	if a.sshPrivateKey != "" {
		dir, err := os.MkdirTemp("", "proxacloud-backup-key-*")
		if err != nil {
			return cleanup, fmt.Errorf("create temporary ssh key directory: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
		a.keyPath = filepath.Join(dir, "id")
		if err := os.WriteFile(a.keyPath, []byte(a.sshPrivateKey+"\n"), 0600); err != nil {
			cleanup()
			return func() {}, fmt.Errorf("write temporary ssh key: %w", err)
		}
		a.sftpCommand = "ssh -i " + a.keyPath + " " + common
		return cleanup, nil
	}
	if a.password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return cleanup, errors.New("sshpass binary is required for password authentication")
		}
		a.sftpCommand = "sshpass -e ssh " + common
		return cleanup, nil
	}
	return cleanup, errors.New("hetzner target requires password or ssh private key authentication")
}

func (a hetznerAuth) rsyncSSHCommand() string {
	base := "ssh -p " + a.port + " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	if a.keyPath != "" {
		base += " -i " + a.keyPath
	}
	return base
}

func runCommand(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return truncateOutput(string(out)), err
}

func (s *Service) updateArtifact(jobID int64, volID string, path string) error {
	_, err := s.db.Exec(
		`UPDATE backup_jobs
		    SET artifact_volid = $2, artifact_path = $3, updated_at = NOW()
		  WHERE id = $1`,
		jobID,
		strings.TrimSpace(volID),
		strings.TrimSpace(path),
	)
	return err
}

func (s *Service) updateUpload(jobID int64, status string, remotePath string, output string, errText string, method string) error {
	if method == "" {
		_, err := s.db.Exec(
			`UPDATE backup_jobs
			    SET upload_status = $2, remote_path = $3, upload_output = $4, upload_error = $5, updated_at = NOW()
			  WHERE id = $1`,
			jobID,
			strings.TrimSpace(status),
			strings.TrimSpace(remotePath),
			truncateOutput(output),
			strings.TrimSpace(errText),
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE backup_jobs
		    SET upload_status = $2, remote_path = $3, upload_output = $4, upload_error = $5, upload_method = $6, updated_at = NOW()
		  WHERE id = $1`,
		jobID,
		strings.TrimSpace(status),
		strings.TrimSpace(remotePath),
		truncateOutput(output),
		strings.TrimSpace(errText),
		strings.TrimSpace(method),
	)
	return err
}

func (s *Service) markUploadFailed(jobID int64, phase string, err error) error {
	message := strings.TrimSpace(phase + ": " + err.Error())
	_ = s.updateUpload(jobID, "failed", "", "", message, "")
	job, getErr := s.getJob(jobID)
	if getErr != nil {
		return getErr
	}
	return s.finishJob(jobID, "failed", job.ProxmoxTaskID, job.Output, message)
}

func backupUploadMethod(target Target) string {
	if target.Type != TargetHetzner {
		return ""
	}
	mode, _ := target.Config["backup_mode"].(string)
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return "restic"
	}
	if mode == "rsync" {
		return "rsync"
	}
	return "restic"
}

func vmIDFromUPID(upid string) (string, error) {
	parts := strings.Split(strings.TrimSpace(upid), ":")
	if len(parts) < 7 {
		return "", fmt.Errorf("invalid proxmox task id %q", upid)
	}
	id := strings.TrimSpace(parts[6])
	if id == "" {
		return "", fmt.Errorf("proxmox task id %q does not include a vmid", upid)
	}
	return id, nil
}

func storageFilename(volID string) string {
	name := strings.TrimSpace(volID)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[idx+1:]
	}
	name = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "_")
	if name == "" {
		return "backup-artifact"
	}
	return name
}

func truncateOutput(out string) string {
	out = strings.TrimSpace(out)
	const limit = 12000
	if len(out) <= limit {
		return out
	}
	return out[len(out)-limit:]
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
	if t == TargetHetzner {
		mode, _ := config["backup_mode"].(string)
		mode = strings.TrimSpace(strings.ToLower(mode))
		if mode != "" && mode != "restic" && mode != "rsync" {
			return fmt.Errorf("unsupported backup_mode %q", mode)
		}
	}
	return nil
}

func validateSecrets(t TargetType, config map[string]any, in TargetInput) error {
	if t != TargetHetzner {
		return nil
	}
	mode, _ := config["backup_mode"].(string)
	if strings.TrimSpace(mode) == "" || strings.EqualFold(strings.TrimSpace(mode), "restic") {
		if strings.TrimSpace(in.ResticPassword) == "" {
			return errors.New("restic_password is required when backup_mode is restic")
		}
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
