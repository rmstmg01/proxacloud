package backups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"proxacloud/backend/internal/tenant"
	_ "time/tzdata"
)

var (
	ErrVPSBackupNotFound = errors.New("VPS backup policy not found")
	ErrVPSBackupDisabled = errors.New("backup protection is not enabled for this VPS")
	ErrBackupInProgress  = errors.New("a backup is already running for this VPS")
)

type BackupBillingGuard interface {
	EnsureCanAffordBackup(accountID int64, previousPlan, nextPlan string, diskGB int) error
}

type vpsBackupSource struct {
	ID                int64
	CustomerAccountID int64
	Name              string
	DiskGB            int
	Status            string
}

type duePolicy struct {
	ID                int64
	VPSID             int64
	CustomerAccountID int64
	TargetID          int64
	Mode              string
	Frequency         string
	Weekday           *int
	MonthDay          *int
	LocalTime         string
	Timezone          string
	ScheduledFor      time.Time
}

func (s *Service) SetBillingGuard(guard BackupBillingGuard) {
	s.billing = guard
}

func (s *Service) ListPlans() ([]Plan, error) {
	rows, err := s.db.Query(`
		SELECT resource_type, display_name, base_monthly_cents, disk_gb_monthly_cents, enabled
		  FROM billing_prices
		 WHERE resource_type IN ('vps_backup_daily', 'vps_backup_weekly', 'vps_backup_monthly')
		 ORDER BY CASE resource_type
		   WHEN 'vps_backup_monthly' THEN 1
		   WHEN 'vps_backup_weekly' THEN 2
		   ELSE 3
		 END`)
	if err != nil {
		return nil, fmt.Errorf("list VPS backup plans: %w", err)
	}
	defer rows.Close()
	plans := []Plan{}
	for rows.Next() {
		var plan Plan
		if err := rows.Scan(&plan.Code, &plan.Name, &plan.BaseMonthlyCents, &plan.DiskGBMonthlyCents, &plan.Enabled); err != nil {
			return nil, err
		}
		plan.Frequency = planFrequency(plan.Code)
		plan.RetentionCount = planRetention(plan.Code)
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *Service) GetVPSBackupOverview(vpsID int64, scope tenant.Scope) (VPSBackupOverview, error) {
	source, err := s.authorizeVPSBackupSource(vpsID, scope)
	if err != nil {
		return VPSBackupOverview{}, err
	}
	plans, err := s.ListPlans()
	if err != nil {
		return VPSBackupOverview{}, err
	}
	policy, err := s.getVPSBackupPolicy(vpsID, source.DiskGB)
	policyFound := err == nil
	if err != nil && !errors.Is(err, ErrVPSBackupNotFound) {
		return VPSBackupOverview{}, err
	}
	jobs, err := s.listVPSBackupJobs(vpsID, scope, 10)
	if err != nil {
		return VPSBackupOverview{}, err
	}
	overview := VPSBackupOverview{Plans: plans, Jobs: jobs}
	if policyFound {
		overview.Policy = &policy
	}
	return overview, nil
}

func (s *Service) UpsertVPSBackupPolicy(vpsID int64, scope tenant.Scope, input UpsertVPSBackupPolicyInput) (VPSBackupPolicy, error) {
	source, err := s.authorizeVPSBackupSource(vpsID, scope)
	if err != nil {
		return VPSBackupPolicy{}, err
	}
	if source.CustomerAccountID <= 0 {
		return VPSBackupPolicy{}, errors.New("VPS must belong to a customer account before backup protection can be enabled")
	}
	planCode := strings.ToLower(strings.TrimSpace(input.PlanCode))
	frequency := planFrequency(planCode)
	retention := planRetention(planCode)
	if frequency == "" || retention == 0 {
		return VPSBackupPolicy{}, errors.New("invalid or disabled VPS backup plan")
	}
	if _, err := s.enabledPlan(planCode); err != nil {
		return VPSBackupPolicy{}, err
	}
	localTime, location, err := validateSchedule(input, frequency)
	if err != nil {
		return VPSBackupPolicy{}, err
	}
	nextRun := nextScheduledRun(time.Now().UTC(), frequency, input.Weekday, input.MonthDay, localTime, location)
	targetID, err := s.defaultVPSBackupTarget()
	if err != nil {
		return VPSBackupPolicy{}, err
	}
	previousPlan := ""
	if existing, getErr := s.getVPSBackupPolicy(vpsID, source.DiskGB); getErr == nil && existing.Enabled {
		previousPlan = existing.PlanCode
	}
	if s.billing != nil {
		if err := s.billing.EnsureCanAffordBackup(source.CustomerAccountID, previousPlan, planCode, source.DiskGB); err != nil {
			return VPSBackupPolicy{}, err
		}
	}

	_, err = s.db.Exec(`
		INSERT INTO vps_backup_policies
			(vps_id, customer_account_id, target_id, plan_code, frequency, weekday, month_day,
			 local_time, timezone, mode, retention_count, enabled, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::time, $9, 'snapshot', $10, TRUE, $11)
		ON CONFLICT (vps_id) DO UPDATE SET
			customer_account_id = EXCLUDED.customer_account_id,
			target_id = EXCLUDED.target_id,
			plan_code = EXCLUDED.plan_code,
			frequency = EXCLUDED.frequency,
			weekday = EXCLUDED.weekday,
			month_day = EXCLUDED.month_day,
			local_time = EXCLUDED.local_time,
			timezone = EXCLUDED.timezone,
			retention_count = EXCLUDED.retention_count,
			enabled = TRUE,
			next_run_at = EXCLUDED.next_run_at,
			last_error = '',
			version = vps_backup_policies.version + 1,
			updated_at = NOW()`,
		vpsID, source.CustomerAccountID, targetID, planCode, frequency,
		nullableInt(input.Weekday), nullableInt(input.MonthDay), localTime.Format("15:04"),
		location.String(), retention, nextRun)
	if err != nil {
		return VPSBackupPolicy{}, fmt.Errorf("save VPS backup policy: %w", err)
	}
	return s.getVPSBackupPolicy(vpsID, source.DiskGB)
}

func (s *Service) DisableVPSBackupPolicy(vpsID int64, scope tenant.Scope) error {
	if _, err := s.authorizeVPSBackupSource(vpsID, scope); err != nil {
		return err
	}
	result, err := s.db.Exec(`
		UPDATE vps_backup_policies
		   SET enabled = FALSE, next_run_at = NULL, version = version + 1, updated_at = NOW()
		 WHERE vps_id = $1`, vpsID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrVPSBackupNotFound
	}
	return nil
}

func (s *Service) StartManualVPSBackup(vpsID int64, scope tenant.Scope) (Job, error) {
	source, err := s.authorizeVPSBackupSource(vpsID, scope)
	if err != nil {
		return Job{}, err
	}
	policy, err := s.getVPSBackupPolicy(vpsID, source.DiskGB)
	if err != nil {
		return Job{}, err
	}
	if !policy.Enabled {
		return Job{}, ErrVPSBackupDisabled
	}
	if err := s.ensureNoActiveVPSBackup(vpsID); err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	job, err := s.StartJob(JobInput{
		TargetID: policy.TargetID, SourceType: SourceVPS, SourceID: vpsID, Mode: policy.Mode,
		PolicyID: policy.ID, CustomerAccountID: source.CustomerAccountID, TriggerType: "manual",
		ScheduledFor: &now, IdempotencyKey: fmt.Sprintf("manual:vps:%d:%d", vpsID, now.UnixNano()),
		RetentionCount: policy.RetentionCount,
	})
	if err != nil {
		return Job{}, err
	}
	_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_run_at = NOW(), updated_at = NOW() WHERE id = $1`, policy.ID)
	return job, nil
}

func (s *Service) RunVPSBackupWorkers(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	s.runVPSBackupCycle(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runVPSBackupCycle(ctx)
		}
	}
}

func (s *Service) runVPSBackupCycle(ctx context.Context) {
	if err := s.reconcileVPSBackupJobs(ctx); err != nil {
		log.Printf("VPS backup reconciliation failed: %v", err)
	}
	due, err := s.claimDueVPSBackupPolicies(ctx, 20)
	if err != nil {
		log.Printf("VPS backup scheduler failed: %v", err)
		return
	}
	for _, policy := range due {
		if err := s.ensureNoActiveVPSBackup(policy.VPSID); err != nil {
			_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_error = $2, updated_at = NOW() WHERE id = $1`, policy.ID, err.Error())
			continue
		}
		_, err := s.StartJob(JobInput{
			TargetID: policy.TargetID, SourceType: SourceVPS, SourceID: policy.VPSID, Mode: policy.Mode,
			PolicyID: policy.ID, CustomerAccountID: policy.CustomerAccountID, TriggerType: "scheduled",
			ScheduledFor:   &policy.ScheduledFor,
			IdempotencyKey: fmt.Sprintf("scheduled:vps-policy:%d:%s", policy.ID, policy.ScheduledFor.UTC().Format(time.RFC3339)),
			RetentionCount: planRetention("vps_backup_" + policy.Frequency),
		})
		if err != nil {
			_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_error = $2, updated_at = NOW() WHERE id = $1`, policy.ID, err.Error())
			continue
		}
		_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_run_at = NOW(), last_error = '', updated_at = NOW() WHERE id = $1`, policy.ID)
	}
}

func (s *Service) claimDueVPSBackupPolicies(ctx context.Context, limit int) ([]duePolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, vps_id, COALESCE(customer_account_id, 0), target_id, mode, frequency,
		       weekday, month_day, local_time::text, timezone, next_run_at
		  FROM vps_backup_policies
		 WHERE enabled = TRUE AND next_run_at IS NOT NULL AND next_run_at <= NOW()
		 ORDER BY next_run_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []duePolicy{}
	for rows.Next() {
		var item duePolicy
		if err := rows.Scan(&item.ID, &item.VPSID, &item.CustomerAccountID, &item.TargetID, &item.Mode,
			&item.Frequency, &item.Weekday, &item.MonthDay, &item.LocalTime, &item.Timezone, &item.ScheduledFor); err != nil {
			return nil, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	claimed := []duePolicy{}
	for _, item := range candidates {
		loc, err := time.LoadLocation(item.Timezone)
		if err != nil {
			loc = time.UTC
		}
		clock, err := time.Parse("15:04:05", normalizeDBTime(item.LocalTime))
		if err != nil {
			continue
		}
		next := nextScheduledRun(time.Now().UTC(), item.Frequency, item.Weekday, item.MonthDay, clock, loc)
		result, err := s.db.ExecContext(ctx, `
			UPDATE vps_backup_policies
			   SET next_run_at = $3, updated_at = NOW()
			 WHERE id = $1 AND enabled = TRUE AND next_run_at = $2`,
			item.ID, item.ScheduledFor, next)
		if err != nil {
			return nil, err
		}
		if affected, _ := result.RowsAffected(); affected == 1 {
			claimed = append(claimed, item)
		}
	}
	return claimed, nil
}

func (s *Service) reconcileVPSBackupJobs(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, proxmox_node_name, proxmox_task_id, COALESCE(policy_id, 0)
		  FROM backup_jobs
		 WHERE source_type = 'vps' AND status = 'running'
		   AND proxmox_task_id <> '' AND upload_method = ''
		 ORDER BY updated_at
		 LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type runningJob struct {
		id, sourceID, policyID int64
		node, taskID           string
	}
	jobs := []runningJob{}
	for rows.Next() {
		var job runningJob
		if err := rows.Scan(&job.id, &job.sourceID, &job.node, &job.taskID, &job.policyID); err != nil {
			return err
		}
		jobs = append(jobs, job)
	}
	for _, job := range jobs {
		source, err := s.resolveBackupSource(SourceVPS, job.sourceID)
		if err != nil {
			continue
		}
		cred, err := s.proxmox.GetCredentialWithSecrets(source.credentialID)
		if err != nil {
			continue
		}
		status, err := s.proxmox.GetTaskStatus(cred, job.node, job.taskID)
		if err != nil || !strings.EqualFold(strings.TrimSpace(status.Status), "stopped") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(status.ExitStatus), "OK") {
			_ = s.finishJob(job.id, "succeeded", job.taskID, "Proxmox vzdump completed successfully", "")
			if job.policyID > 0 {
				_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_success_at = NOW(), last_error = '', updated_at = NOW() WHERE id = $1`, job.policyID)
			}
		} else {
			message := "Proxmox backup failed: " + strings.TrimSpace(status.ExitStatus)
			_ = s.finishJob(job.id, "failed", job.taskID, "", message)
			if job.policyID > 0 {
				_, _ = s.db.Exec(`UPDATE vps_backup_policies SET last_error = $2, updated_at = NOW() WHERE id = $1`, job.policyID, message)
			}
		}
	}
	return rows.Err()
}

func (s *Service) authorizeVPSBackupSource(vpsID int64, scope tenant.Scope) (vpsBackupSource, error) {
	var source vpsBackupSource
	query := `SELECT id, COALESCE(customer_account_id, 0), name, disk_gb, status FROM vps_instances WHERE id = $1`
	args := []any{vpsID}
	if scope.IsCustomer() {
		query += ` AND customer_account_id = $2`
		args = append(args, scope.CustomerAccountID)
	}
	err := s.db.QueryRow(query, args...).Scan(&source.ID, &source.CustomerAccountID, &source.Name, &source.DiskGB, &source.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return vpsBackupSource{}, errors.New("VPS not found")
	}
	if err != nil {
		return vpsBackupSource{}, err
	}
	if strings.EqualFold(source.Status, "deleted") || strings.Contains(strings.ToLower(source.Status), "delet") {
		return vpsBackupSource{}, errors.New("backup protection cannot be changed while the VPS is being deleted")
	}
	return source, nil
}

func (s *Service) defaultVPSBackupTarget() (int64, error) {
	var id int64
	err := s.db.QueryRow(`
		SELECT id FROM backup_targets
		 WHERE enabled = TRUE AND type = 'proxmox_storage'
		 ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("no enabled Proxmox backup storage target is configured")
	}
	return id, err
}

func (s *Service) enabledPlan(code string) (Plan, error) {
	var plan Plan
	err := s.db.QueryRow(`
		SELECT resource_type, display_name, base_monthly_cents, disk_gb_monthly_cents, enabled
		  FROM billing_prices WHERE resource_type = $1 AND enabled = TRUE`, code).
		Scan(&plan.Code, &plan.Name, &plan.BaseMonthlyCents, &plan.DiskGBMonthlyCents, &plan.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, errors.New("selected VPS backup plan is not available")
	}
	plan.Frequency = planFrequency(code)
	plan.RetentionCount = planRetention(code)
	return plan, err
}

func (s *Service) getVPSBackupPolicy(vpsID int64, diskGB int) (VPSBackupPolicy, error) {
	var policy VPSBackupPolicy
	err := s.db.QueryRow(`
		SELECT p.id, p.vps_id, COALESCE(p.customer_account_id, 0), p.target_id, t.name,
		       p.plan_code, p.frequency, p.weekday, p.month_day, p.local_time::text,
		       p.timezone, p.mode, p.retention_count, p.enabled,
		       COALESCE(bp.base_monthly_cents, 0) + (COALESCE(bp.disk_gb_monthly_cents, 0) * $2),
		       p.next_run_at, p.last_run_at, p.last_success_at, p.last_error,
		       p.version, p.created_at, p.updated_at
		  FROM vps_backup_policies p
		  JOIN backup_targets t ON t.id = p.target_id
		  LEFT JOIN billing_prices bp ON bp.resource_type = p.plan_code
		 WHERE p.vps_id = $1`, vpsID, diskGB).
		Scan(&policy.ID, &policy.VPSID, &policy.CustomerAccountID, &policy.TargetID, &policy.TargetName,
			&policy.PlanCode, &policy.Frequency, &policy.Weekday, &policy.MonthDay, &policy.LocalTime,
			&policy.Timezone, &policy.Mode, &policy.RetentionCount, &policy.Enabled,
			&policy.MonthlyPriceCents, &policy.NextRunAt, &policy.LastRunAt, &policy.LastSuccessAt,
			&policy.LastError, &policy.Version, &policy.CreatedAt, &policy.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return VPSBackupPolicy{}, ErrVPSBackupNotFound
	}
	policy.LocalTime = strings.TrimSuffix(normalizeDBTime(policy.LocalTime), ":00")
	return policy, err
}

func (s *Service) listVPSBackupJobs(vpsID int64, scope tenant.Scope, limit int) ([]Job, error) {
	query := `
		SELECT j.id, j.target_id, COALESCE(t.name, ''), j.source_type, j.source_id, j.source_name,
		       j.mode, j.status, j.proxmox_node_name, j.proxmox_task_id, j.staging_storage_id,
		       j.artifact_volid, j.artifact_path, j.upload_method, j.upload_status, j.remote_path,
		       j.upload_output, j.upload_error, j.output, j.error,
		       COALESCE(j.policy_id, 0), COALESCE(j.customer_account_id, 0), j.trigger_type,
		       j.scheduled_for, j.started_at, j.finished_at, j.created_at, j.updated_at
		  FROM backup_jobs j LEFT JOIN backup_targets t ON t.id = j.target_id
		 WHERE j.source_type = 'vps' AND j.source_id = $1`
	args := []any{vpsID}
	if scope.IsCustomer() {
		query += ` AND j.customer_account_id = $2`
		args = append(args, scope.CustomerAccountID)
	}
	query += fmt.Sprintf(" ORDER BY j.created_at DESC LIMIT %d", limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.TargetID, &job.TargetName, &job.SourceType, &job.SourceID,
			&job.SourceName, &job.Mode, &job.Status, &job.ProxmoxNodeName, &job.ProxmoxTaskID,
			&job.StagingStorageID, &job.ArtifactVolID, &job.ArtifactPath, &job.UploadMethod,
			&job.UploadStatus, &job.RemotePath, &job.UploadOutput, &job.UploadError, &job.Output,
			&job.Error, &job.PolicyID, &job.CustomerAccountID, &job.TriggerType, &job.ScheduledFor,
			&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Service) ensureNoActiveVPSBackup(vpsID int64) error {
	var count int
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM backup_jobs
		 WHERE source_type = 'vps' AND source_id = $1
		   AND status IN ('pending', 'running', 'uploading')`, vpsID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrBackupInProgress
	}
	return nil
}

func validateSchedule(input UpsertVPSBackupPolicyInput, frequency string) (time.Time, *time.Location, error) {
	clock, err := time.Parse("15:04", strings.TrimSpace(input.LocalTime))
	if err != nil {
		return time.Time{}, nil, errors.New("local_time must use HH:MM 24-hour format")
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, nil, errors.New("invalid IANA timezone")
	}
	if frequency == "weekly" && (input.Weekday == nil || *input.Weekday < 0 || *input.Weekday > 6) {
		return time.Time{}, nil, errors.New("weekday between 0 and 6 is required for weekly backups")
	}
	if frequency == "monthly" && (input.MonthDay == nil || *input.MonthDay < 1 || *input.MonthDay > 28) {
		return time.Time{}, nil, errors.New("month_day between 1 and 28 is required for monthly backups")
	}
	return clock, location, nil
}

func nextScheduledRun(now time.Time, frequency string, weekday, monthDay *int, clock time.Time, location *time.Location) time.Time {
	localNow := now.In(location)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	switch frequency {
	case "weekly":
		target := time.Weekday(*weekday)
		days := (int(target) - int(candidate.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, days)
		if !candidate.After(localNow) {
			candidate = candidate.AddDate(0, 0, 7)
		}
	case "monthly":
		candidate = time.Date(localNow.Year(), localNow.Month(), *monthDay, clock.Hour(), clock.Minute(), 0, 0, location)
		if !candidate.After(localNow) {
			candidate = candidate.AddDate(0, 1, 0)
		}
	default:
		if !candidate.After(localNow) {
			candidate = candidate.AddDate(0, 0, 1)
		}
	}
	return candidate.UTC()
}

func planFrequency(code string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(code)), "vps_backup_")
}

func planRetention(code string) int {
	switch planFrequency(code) {
	case "daily":
		return 7
	case "weekly":
		return 4
	case "monthly":
		return 3
	default:
		return 0
	}
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizeDBTime(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 8 {
		return value[:8]
	}
	if len(value) == 5 {
		return value + ":00"
	}
	return value
}
