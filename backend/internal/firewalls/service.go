package firewalls

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"

	"proxacloud/backend/internal/proxmox"
)

var (
	ErrGroupNotFound      = errors.New("firewall group not found")
	ErrRuleNotFound       = errors.New("firewall rule not found")
	ErrAttachmentNotFound = errors.New("firewall attachment not found")
	ErrTargetNotFound     = errors.New("firewall target not found")
	ErrTargetUnsupported  = errors.New("firewall target type is not supported")
)

type Service struct {
	db      *sql.DB
	proxmox ProxmoxFirewallApplier
}

type ProxmoxFirewallApplier interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	ApplyVMFirewallByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, rules []proxmox.FirewallRuleSpec, managedPrefix string) (string, error)
	ApplyContainerFirewallByName(cred proxmox.Credential, proxmoxNodeName string, containerName string, rules []proxmox.FirewallRuleSpec, managedPrefix string) (string, error)
	ClearVMFirewallByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, managedPrefix string) (string, error)
	ClearContainerFirewallByName(cred proxmox.Credential, proxmoxNodeName string, containerName string, managedPrefix string) (string, error)
}

func NewService(db *sql.DB, proxmoxApplier ProxmoxFirewallApplier) *Service {
	return &Service{db: db, proxmox: proxmoxApplier}
}

func (s *Service) List() ([]Group, error) {
	rows, err := s.db.Query(
		`SELECT id, name, description, default_inbound_policy, default_outbound_policy, enabled, status, last_applied_at, created_at, updated_at
		 FROM firewall_groups
		 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list firewall groups: %w", err)
	}
	defer rows.Close()

	out := []Group{}
	for rows.Next() {
		var rec Group
		if err := scanGroup(rows, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) Get(id int64) (Group, error) {
	var rec Group
	err := s.db.QueryRow(
		`SELECT id, name, description, default_inbound_policy, default_outbound_policy, enabled, status, last_applied_at, created_at, updated_at
		 FROM firewall_groups
		 WHERE id = $1`,
		id,
	).Scan(&rec.ID, &rec.Name, &rec.Description, &rec.DefaultInboundPolicy, &rec.DefaultOutboundPolicy, &rec.Enabled, &rec.Status, &rec.LastAppliedAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Group{}, ErrGroupNotFound
		}
		return Group{}, fmt.Errorf("get firewall group: %w", err)
	}

	rules, err := s.ListRules(id)
	if err != nil {
		return Group{}, err
	}
	attachments, err := s.ListAttachments(id)
	if err != nil {
		return Group{}, err
	}
	rec.Rules = rules
	rec.Attachments = attachments
	return rec, nil
}

func (s *Service) Create(req CreateGroupRequest) (Group, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.DefaultInboundPolicy = defaultString(strings.TrimSpace(req.DefaultInboundPolicy), PolicyDeny)
	req.DefaultOutboundPolicy = defaultString(strings.TrimSpace(req.DefaultOutboundPolicy), PolicyAllow)
	if err := validateGroup(req.Name, req.DefaultInboundPolicy, req.DefaultOutboundPolicy); err != nil {
		return Group{}, err
	}

	var rec Group
	err := s.db.QueryRow(
		`INSERT INTO firewall_groups (name, description, default_inbound_policy, default_outbound_policy, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, description, default_inbound_policy, default_outbound_policy, enabled, status, last_applied_at, created_at, updated_at`,
		req.Name,
		req.Description,
		req.DefaultInboundPolicy,
		req.DefaultOutboundPolicy,
		req.Enabled,
	).Scan(&rec.ID, &rec.Name, &rec.Description, &rec.DefaultInboundPolicy, &rec.DefaultOutboundPolicy, &rec.Enabled, &rec.Status, &rec.LastAppliedAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("create firewall group: %w", err)
	}
	_ = s.addEvent(rec.ID, "create", "info", "Firewall group created", "")
	return rec, nil
}

func (s *Service) Update(id int64, req UpdateGroupRequest) (Group, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.DefaultInboundPolicy = defaultString(strings.TrimSpace(req.DefaultInboundPolicy), PolicyDeny)
	req.DefaultOutboundPolicy = defaultString(strings.TrimSpace(req.DefaultOutboundPolicy), PolicyAllow)
	if err := validateGroup(req.Name, req.DefaultInboundPolicy, req.DefaultOutboundPolicy); err != nil {
		return Group{}, err
	}

	var rec Group
	err := s.db.QueryRow(
		`UPDATE firewall_groups
		 SET name = $2, description = $3, default_inbound_policy = $4, default_outbound_policy = $5, enabled = $6, status = 'modified', updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, name, description, default_inbound_policy, default_outbound_policy, enabled, status, last_applied_at, created_at, updated_at`,
		id,
		req.Name,
		req.Description,
		req.DefaultInboundPolicy,
		req.DefaultOutboundPolicy,
		req.Enabled,
	).Scan(&rec.ID, &rec.Name, &rec.Description, &rec.DefaultInboundPolicy, &rec.DefaultOutboundPolicy, &rec.Enabled, &rec.Status, &rec.LastAppliedAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Group{}, ErrGroupNotFound
		}
		return Group{}, fmt.Errorf("update firewall group: %w", err)
	}
	_ = s.addEvent(id, "update", "info", "Firewall group updated", "")
	group, err := s.Get(id)
	if err != nil {
		return Group{}, err
	}
	if _, err := s.syncGroup(group, "group.update"); err != nil {
		return group, err
	}
	return group, nil
}

func (s *Service) Delete(id int64) error {
	group, err := s.Get(id)
	if err != nil {
		return err
	}
	if len(group.Attachments) > 0 {
		outputs, err := s.clearGroupFirewall(group)
		if err != nil {
			_ = s.addEvent(id, "delete.cleanup", "error", "Failed to remove Proxmox firewall rules before delete", err.Error())
			return err
		}
		_ = s.addEvent(id, "delete.cleanup", "success", "Removed Proxmox firewall rules before delete", strings.Join(outputs, "\n"))
	}

	res, err := s.db.Exec(`DELETE FROM firewall_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete firewall group: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrGroupNotFound
	}
	return nil
}

func (s *Service) ListRules(groupID int64) ([]Rule, error) {
	rows, err := s.db.Query(
		`SELECT id, firewall_group_id, direction, action, protocol, source_cidr, destination_cidr, port_start, port_end, priority, description, enabled, created_at, updated_at
		 FROM firewall_rules
		 WHERE firewall_group_id = $1
		 ORDER BY priority ASC, id ASC`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("list firewall rules: %w", err)
	}
	defer rows.Close()

	out := []Rule{}
	for rows.Next() {
		var rec Rule
		if err := scanRule(rows, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) CreateRule(groupID int64, req CreateRuleRequest) (Rule, error) {
	if _, err := s.Get(groupID); err != nil {
		return Rule{}, err
	}
	rule := ruleFromRequest(groupID, req)
	if err := validateRule(rule); err != nil {
		return Rule{}, err
	}

	var rec Rule
	err := s.db.QueryRow(
		`INSERT INTO firewall_rules (firewall_group_id, direction, action, protocol, source_cidr, destination_cidr, port_start, port_end, priority, description, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, firewall_group_id, direction, action, protocol, source_cidr, destination_cidr, port_start, port_end, priority, description, enabled, created_at, updated_at`,
		rule.FirewallGroupID,
		rule.Direction,
		rule.Action,
		rule.Protocol,
		rule.SourceCIDR,
		rule.DestinationCIDR,
		rule.PortStart,
		rule.PortEnd,
		rule.Priority,
		rule.Description,
		rule.Enabled,
	).Scan(&rec.ID, &rec.FirewallGroupID, &rec.Direction, &rec.Action, &rec.Protocol, &rec.SourceCIDR, &rec.DestinationCIDR, &rec.PortStart, &rec.PortEnd, &rec.Priority, &rec.Description, &rec.Enabled, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return Rule{}, fmt.Errorf("create firewall rule: %w", err)
	}
	_ = s.markModified(groupID)
	_ = s.addEvent(groupID, "rule.create", "info", "Firewall rule added", "")
	if _, err := s.syncGroupByID(groupID, "rule.create.sync"); err != nil {
		return rec, err
	}
	return rec, nil
}

func (s *Service) UpdateRule(groupID, ruleID int64, req UpdateRuleRequest) (Rule, error) {
	rule := ruleFromRequest(groupID, req)
	if err := validateRule(rule); err != nil {
		return Rule{}, err
	}

	var rec Rule
	err := s.db.QueryRow(
		`UPDATE firewall_rules
		 SET direction = $3, action = $4, protocol = $5, source_cidr = $6, destination_cidr = $7, port_start = $8, port_end = $9, priority = $10, description = $11, enabled = $12, updated_at = NOW()
		 WHERE firewall_group_id = $1 AND id = $2
		 RETURNING id, firewall_group_id, direction, action, protocol, source_cidr, destination_cidr, port_start, port_end, priority, description, enabled, created_at, updated_at`,
		groupID,
		ruleID,
		rule.Direction,
		rule.Action,
		rule.Protocol,
		rule.SourceCIDR,
		rule.DestinationCIDR,
		rule.PortStart,
		rule.PortEnd,
		rule.Priority,
		rule.Description,
		rule.Enabled,
	).Scan(&rec.ID, &rec.FirewallGroupID, &rec.Direction, &rec.Action, &rec.Protocol, &rec.SourceCIDR, &rec.DestinationCIDR, &rec.PortStart, &rec.PortEnd, &rec.Priority, &rec.Description, &rec.Enabled, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Rule{}, ErrRuleNotFound
		}
		return Rule{}, fmt.Errorf("update firewall rule: %w", err)
	}
	_ = s.markModified(groupID)
	_ = s.addEvent(groupID, "rule.update", "info", "Firewall rule updated", "")
	if _, err := s.syncGroupByID(groupID, "rule.update.sync"); err != nil {
		return rec, err
	}
	return rec, nil
}

func (s *Service) DeleteRule(groupID, ruleID int64) error {
	res, err := s.db.Exec(`DELETE FROM firewall_rules WHERE firewall_group_id = $1 AND id = $2`, groupID, ruleID)
	if err != nil {
		return fmt.Errorf("delete firewall rule: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrRuleNotFound
	}
	_ = s.markModified(groupID)
	_ = s.addEvent(groupID, "rule.delete", "info", "Firewall rule deleted", "")
	if _, err := s.syncGroupByID(groupID, "rule.delete.sync"); err != nil {
		return err
	}
	return nil
}

func (s *Service) ListAttachments(groupID int64) ([]Attachment, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.firewall_group_id, a.target_type, a.target_id,
		        CASE
		            WHEN a.target_type = 'vps' THEN COALESCE((SELECT name FROM vps_instances WHERE id = a.target_id), '')
		            WHEN a.target_type = 'container' THEN COALESCE((SELECT name FROM container_instances WHERE id = a.target_id), '')
		            ELSE ''
		        END AS target_name,
		        a.created_at
		 FROM firewall_attachments a
		 WHERE a.firewall_group_id = $1
		 ORDER BY a.target_type ASC, target_name ASC, a.id ASC`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("list firewall attachments: %w", err)
	}
	defer rows.Close()

	out := []Attachment{}
	for rows.Next() {
		var rec Attachment
		if err := rows.Scan(&rec.ID, &rec.FirewallGroupID, &rec.TargetType, &rec.TargetID, &rec.TargetName, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) CreateAttachment(groupID int64, req CreateAttachmentRequest) (Attachment, error) {
	if _, err := s.Get(groupID); err != nil {
		return Attachment{}, err
	}
	req.TargetType = strings.TrimSpace(req.TargetType)
	if err := s.validateTarget(req.TargetType, req.TargetID); err != nil {
		return Attachment{}, err
	}

	var rec Attachment
	err := s.db.QueryRow(
		`INSERT INTO firewall_attachments (firewall_group_id, target_type, target_id)
		 VALUES ($1, $2, $3)
		 RETURNING id, firewall_group_id, target_type, target_id, created_at`,
		groupID,
		req.TargetType,
		req.TargetID,
	).Scan(&rec.ID, &rec.FirewallGroupID, &rec.TargetType, &rec.TargetID, &rec.CreatedAt)
	if err != nil {
		return Attachment{}, fmt.Errorf("create firewall attachment: %w", err)
	}
	_ = s.markModified(groupID)
	_ = s.addEvent(groupID, "attachment.create", "info", "Firewall attachment added", fmt.Sprintf("%s:%d", req.TargetType, req.TargetID))
	group, err := s.Get(groupID)
	if err != nil {
		return rec, err
	}
	resp, err := s.syncGroup(group, "attachment.create.sync")
	if err != nil {
		_, _ = s.db.Exec(`DELETE FROM firewall_attachments WHERE id = $1`, rec.ID)
		_ = s.addEvent(groupID, "attachment.apply", "error", "Failed to apply firewall to attached target", err.Error())
		return rec, err
	}
	_ = s.addEvent(groupID, "attachment.apply", "success", "Firewall applied to attached target", resp.Message)
	return rec, nil
}

func (s *Service) DeleteAttachment(groupID, attachmentID int64) error {
	group, err := s.Get(groupID)
	if err != nil {
		return err
	}
	attachment, err := findAttachment(group.Attachments, attachmentID)
	if err != nil {
		return err
	}
	output, err := s.clearAttachmentFirewall(group, attachment)
	if err != nil {
		if isCleanupSkippableAttachmentError(err) {
			output = fmt.Sprintf("%s:%d: target unavailable, skipped Proxmox cleanup", attachment.TargetType, attachment.TargetID)
			_ = s.addEvent(groupID, "attachment.detach", "warning", "Detached stale firewall target", err.Error())
		} else {
			_ = s.addEvent(groupID, "attachment.detach", "error", "Failed to remove firewall rules from detached target", err.Error())
			return err
		}
	}

	res, err := s.db.Exec(`DELETE FROM firewall_attachments WHERE firewall_group_id = $1 AND id = $2`, groupID, attachmentID)
	if err != nil {
		return fmt.Errorf("delete firewall attachment: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrAttachmentNotFound
	}
	_ = s.markModified(groupID)
	_ = s.addEvent(groupID, "attachment.delete", "info", "Firewall attachment removed", output)
	return nil
}

func (s *Service) Apply(groupID int64) (ApplyResponse, error) {
	group, err := s.Get(groupID)
	if err != nil {
		return ApplyResponse{}, err
	}
	return s.syncGroup(group, "sync")
}

func (s *Service) syncGroupByID(groupID int64, eventAction string) (ApplyResponse, error) {
	group, err := s.Get(groupID)
	if err != nil {
		return ApplyResponse{}, err
	}
	return s.syncGroup(group, eventAction)
}

func (s *Service) syncGroup(group Group, eventAction string) (ApplyResponse, error) {
	warnings := []string{}
	if len(group.Attachments) == 0 {
		warnings = append(warnings, "firewall has no attached targets")
	}
	if s.proxmox == nil {
		return ApplyResponse{}, errors.New("proxmox firewall applier is not configured")
	}
	if !group.Enabled {
		outputs, err := s.clearGroupFirewall(group)
		if err != nil {
			_, _ = s.db.Exec(`UPDATE firewall_groups SET status = 'failed', updated_at = NOW() WHERE id = $1`, group.ID)
			_ = s.addEvent(group.ID, eventAction, "error", "Disabled firewall cleanup failed", err.Error())
			return ApplyResponse{FirewallGroupID: group.ID, Status: "failed", Message: "Disabled firewall cleanup failed", Warnings: []string{err.Error()}}, err
		}
		_, err = s.db.Exec(`UPDATE firewall_groups SET status = 'disabled', last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, group.ID)
		if err != nil {
			return ApplyResponse{}, fmt.Errorf("mark firewall disabled: %w", err)
		}
		_ = s.addEvent(group.ID, eventAction, "success", "Firewall disabled and managed rules removed", strings.Join(outputs, "\n"))
		return ApplyResponse{FirewallGroupID: group.ID, Status: "disabled", Message: "Firewall disabled and managed rules removed", Warnings: warnings}, nil
	}

	rules := proxmoxRulesFromGroup(group)
	outputs := []string{}
	failures := []string{}
	for _, attachment := range group.Attachments {
		output, applyErr := s.applyAttachmentFirewall(group, attachment, rules)
		if applyErr != nil {
			failures = append(failures, fmt.Sprintf("%s:%d: %v", attachment.TargetType, attachment.TargetID, applyErr))
			continue
		}
		outputs = append(outputs, output)
	}

	status := "applied"
	level := "success"
	message := fmt.Sprintf("Firewall applied to %d target(s)", len(outputs))
	if len(failures) > 0 {
		status = "failed"
		level = "error"
		message = fmt.Sprintf("Firewall apply failed for %d target(s)", len(failures))
		if len(outputs) > 0 {
			status = "partial"
			level = "warning"
			message = fmt.Sprintf("Firewall applied to %d target(s), failed for %d target(s)", len(outputs), len(failures))
		}
		warnings = append(warnings, failures...)
	}
	if len(group.Attachments) == 0 {
		status = "applied"
		level = "warning"
		message = "Firewall has no attached targets"
	}

	_, err := s.db.Exec(`UPDATE firewall_groups SET status = $2, last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, group.ID, status)
	if err != nil {
		return ApplyResponse{}, fmt.Errorf("mark firewall apply result: %w", err)
	}
	_ = s.addEvent(group.ID, eventAction, level, message, strings.Join(append(outputs, failures...), "\n"))
	if len(failures) > 0 && len(outputs) == 0 {
		return ApplyResponse{FirewallGroupID: group.ID, Status: status, Message: message, Warnings: warnings}, errors.New(message)
	}
	return ApplyResponse{FirewallGroupID: group.ID, Status: status, Message: message, Warnings: warnings}, nil
}

func (s *Service) applyAttachmentFirewall(group Group, attachment Attachment, rules []proxmox.FirewallRuleSpec) (string, error) {
	managedPrefix := managedRulePrefix(group.ID)
	switch attachment.TargetType {
	case TargetVPS:
		var credentialID int64
		var nodeName, vmName string
		err := s.db.QueryRow(`SELECT proxmox_credential_id, proxmox_node_name, vm_name FROM vps_instances WHERE id = $1`, attachment.TargetID).Scan(&credentialID, &nodeName, &vmName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", targetNotFoundError(TargetVPS)
			}
			return "", fmt.Errorf("load vps target: %w", err)
		}
		cred, err := s.proxmox.GetCredentialWithSecrets(credentialID)
		if err != nil {
			return "", err
		}
		return s.proxmox.ApplyVMFirewallByName(cred, nodeName, vmName, rules, managedPrefix)
	case TargetContainer:
		var credentialID int64
		var nodeName, containerName string
		err := s.db.QueryRow(`SELECT proxmox_credential_id, proxmox_node_name, container_name FROM container_instances WHERE id = $1`, attachment.TargetID).Scan(&credentialID, &nodeName, &containerName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", targetNotFoundError(TargetContainer)
			}
			return "", fmt.Errorf("load container target: %w", err)
		}
		cred, err := s.proxmox.GetCredentialWithSecrets(credentialID)
		if err != nil {
			return "", err
		}
		return s.proxmox.ApplyContainerFirewallByName(cred, nodeName, containerName, rules, managedPrefix)
	default:
		return "", targetUnsupportedError(attachment.TargetType, "apply")
	}
}

func (s *Service) clearGroupFirewall(group Group) ([]string, error) {
	if s.proxmox == nil {
		return nil, errors.New("proxmox firewall applier is not configured")
	}
	outputs := []string{}
	failures := []string{}
	for _, attachment := range group.Attachments {
		output, err := s.clearAttachmentFirewall(group, attachment)
		if err != nil {
			if isCleanupSkippableAttachmentError(err) {
				outputs = append(outputs, fmt.Sprintf("%s:%d: target unavailable, skipped Proxmox cleanup", attachment.TargetType, attachment.TargetID))
				continue
			}
			failures = append(failures, fmt.Sprintf("%s:%d: %v", attachment.TargetType, attachment.TargetID, err))
			continue
		}
		outputs = append(outputs, output)
	}
	if len(failures) > 0 {
		return outputs, fmt.Errorf("remove proxmox firewall rules: %s", strings.Join(failures, "; "))
	}
	return outputs, nil
}

func (s *Service) clearAttachmentFirewall(group Group, attachment Attachment) (string, error) {
	managedPrefix := managedRulePrefix(group.ID)
	switch attachment.TargetType {
	case TargetVPS:
		var credentialID int64
		var nodeName, vmName string
		err := s.db.QueryRow(`SELECT proxmox_credential_id, proxmox_node_name, vm_name FROM vps_instances WHERE id = $1`, attachment.TargetID).Scan(&credentialID, &nodeName, &vmName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", targetNotFoundError(TargetVPS)
			}
			return "", fmt.Errorf("load vps target: %w", err)
		}
		cred, err := s.proxmox.GetCredentialWithSecrets(credentialID)
		if err != nil {
			return "", err
		}
		return s.proxmox.ClearVMFirewallByName(cred, nodeName, vmName, managedPrefix)
	case TargetContainer:
		var credentialID int64
		var nodeName, containerName string
		err := s.db.QueryRow(`SELECT proxmox_credential_id, proxmox_node_name, container_name FROM container_instances WHERE id = $1`, attachment.TargetID).Scan(&credentialID, &nodeName, &containerName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", targetNotFoundError(TargetContainer)
			}
			return "", fmt.Errorf("load container target: %w", err)
		}
		cred, err := s.proxmox.GetCredentialWithSecrets(credentialID)
		if err != nil {
			return "", err
		}
		return s.proxmox.ClearContainerFirewallByName(cred, nodeName, containerName, managedPrefix)
	default:
		return "", targetUnsupportedError(attachment.TargetType, "cleanup")
	}
}

func targetNotFoundError(targetType string) error {
	return fmt.Errorf("%s target not found: %w", targetType, ErrTargetNotFound)
}

func targetUnsupportedError(targetType string, operation string) error {
	return fmt.Errorf("target type %q is not supported for %s: %w", targetType, operation, ErrTargetUnsupported)
}

func isCleanupSkippableAttachmentError(err error) bool {
	return errors.Is(err, ErrTargetNotFound) || errors.Is(err, ErrTargetUnsupported)
}

func findAttachment(attachments []Attachment, attachmentID int64) (Attachment, error) {
	for _, attachment := range attachments {
		if attachment.ID == attachmentID {
			return attachment, nil
		}
	}
	return Attachment{}, ErrAttachmentNotFound
}

func proxmoxRulesFromGroup(group Group) []proxmox.FirewallRuleSpec {
	out := []proxmox.FirewallRuleSpec{}
	for _, rule := range group.Rules {
		if !rule.Enabled {
			continue
		}
		out = append(out, proxmox.FirewallRuleSpec{
			Direction:       rule.Direction,
			Action:          rule.Action,
			Protocol:        rule.Protocol,
			SourceCIDR:      rule.SourceCIDR,
			DestinationCIDR: rule.DestinationCIDR,
			PortStart:       rule.PortStart,
			PortEnd:         rule.PortEnd,
			Priority:        rule.Priority,
			Description:     rule.Description,
			Enabled:         rule.Enabled,
			ManagedComment:  managedRuleComment(group.ID, rule),
		})
	}
	return out
}

func managedRulePrefix(groupID int64) string {
	return fmt.Sprintf("proxacloud firewall:%d", groupID)
}

func managedRuleComment(groupID int64, rule Rule) string {
	description := strings.TrimSpace(rule.Description)
	if description == "" {
		return fmt.Sprintf("%s rule:%d", managedRulePrefix(groupID), rule.ID)
	}
	return fmt.Sprintf("%s rule:%d %s", managedRulePrefix(groupID), rule.ID, description)
}

func (s *Service) ListEvents(groupID int64) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, firewall_group_id, action, level, message, output, created_at
		 FROM firewall_events
		 WHERE firewall_group_id = $1
		 ORDER BY created_at DESC
		 LIMIT 200`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("list firewall events: %w", err)
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var rec Event
		if err := rows.Scan(&rec.ID, &rec.FirewallGroupID, &rec.Action, &rec.Level, &rec.Message, &rec.Output, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) validateTarget(targetType string, targetID int64) error {
	if targetID <= 0 {
		return errors.New("target_id must be greater than zero")
	}

	var query string
	switch targetType {
	case TargetVPS:
		query = `SELECT EXISTS (SELECT 1 FROM vps_instances WHERE id = $1)`
	case TargetContainer:
		query = `SELECT EXISTS (SELECT 1 FROM container_instances WHERE id = $1)`
	case TargetClusterNode, TargetDatabaseNode:
		return errors.New("target type is reserved for a later firewall phase")
	default:
		return errors.New("target_type must be vps or container")
	}

	var exists bool
	if err := s.db.QueryRow(query, targetID).Scan(&exists); err != nil {
		return fmt.Errorf("validate firewall target: %w", err)
	}
	if !exists {
		return fmt.Errorf("%s target not found", targetType)
	}
	return nil
}

func (s *Service) markModified(groupID int64) error {
	_, err := s.db.Exec(`UPDATE firewall_groups SET status = 'modified', updated_at = NOW() WHERE id = $1`, groupID)
	return err
}

func (s *Service) addEvent(groupID int64, action, level, message, output string) error {
	_, err := s.db.Exec(
		`INSERT INTO firewall_events (firewall_group_id, action, level, message, output) VALUES ($1, $2, $3, $4, $5)`,
		groupID,
		action,
		level,
		message,
		output,
	)
	return err
}

func scanGroup(scanner interface {
	Scan(dest ...any) error
}, rec *Group) error {
	return scanner.Scan(&rec.ID, &rec.Name, &rec.Description, &rec.DefaultInboundPolicy, &rec.DefaultOutboundPolicy, &rec.Enabled, &rec.Status, &rec.LastAppliedAt, &rec.CreatedAt, &rec.UpdatedAt)
}

func scanRule(scanner interface {
	Scan(dest ...any) error
}, rec *Rule) error {
	return scanner.Scan(&rec.ID, &rec.FirewallGroupID, &rec.Direction, &rec.Action, &rec.Protocol, &rec.SourceCIDR, &rec.DestinationCIDR, &rec.PortStart, &rec.PortEnd, &rec.Priority, &rec.Description, &rec.Enabled, &rec.CreatedAt, &rec.UpdatedAt)
}

func ruleFromRequest(groupID int64, req CreateRuleRequest) Rule {
	return Rule{
		FirewallGroupID: groupID,
		Direction:       strings.TrimSpace(req.Direction),
		Action:          strings.TrimSpace(req.Action),
		Protocol:        strings.TrimSpace(req.Protocol),
		SourceCIDR:      strings.TrimSpace(req.SourceCIDR),
		DestinationCIDR: strings.TrimSpace(req.DestinationCIDR),
		PortStart:       req.PortStart,
		PortEnd:         req.PortEnd,
		Priority:        req.Priority,
		Description:     strings.TrimSpace(req.Description),
		Enabled:         req.Enabled,
	}
}

func validateGroup(name, inboundPolicy, outboundPolicy string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if !oneOf(inboundPolicy, PolicyAllow, PolicyDeny) {
		return errors.New("default_inbound_policy must be allow or deny")
	}
	if !oneOf(outboundPolicy, PolicyAllow, PolicyDeny) {
		return errors.New("default_outbound_policy must be allow or deny")
	}
	return nil
}

func validateRule(rule Rule) error {
	if !oneOf(rule.Direction, DirectionInbound, DirectionOutbound) {
		return errors.New("direction must be inbound or outbound")
	}
	if !oneOf(rule.Action, ActionAllow, ActionDeny) {
		return errors.New("action must be allow or deny")
	}
	if !oneOf(rule.Protocol, ProtocolTCP, ProtocolUDP, ProtocolICMP, ProtocolAny) {
		return errors.New("protocol must be tcp, udp, icmp, or any")
	}
	if rule.Priority <= 0 {
		return errors.New("priority must be greater than zero")
	}
	if rule.SourceCIDR != "" {
		if _, _, err := net.ParseCIDR(rule.SourceCIDR); err != nil {
			return errors.New("source_cidr must be a valid CIDR")
		}
	}
	if rule.DestinationCIDR != "" {
		if _, _, err := net.ParseCIDR(rule.DestinationCIDR); err != nil {
			return errors.New("destination_cidr must be a valid CIDR")
		}
	}
	if rule.Protocol == ProtocolICMP || rule.Protocol == ProtocolAny {
		if rule.PortStart != nil || rule.PortEnd != nil {
			return errors.New("ports are only valid for tcp and udp rules")
		}
		return nil
	}
	if rule.PortStart == nil && rule.PortEnd == nil {
		return nil
	}
	if rule.PortStart == nil || rule.PortEnd == nil {
		return errors.New("port_start and port_end must be provided together")
	}
	if *rule.PortStart < 1 || *rule.PortStart > 65535 || *rule.PortEnd < 1 || *rule.PortEnd > 65535 {
		return errors.New("ports must be between 1 and 65535")
	}
	if *rule.PortStart > *rule.PortEnd {
		return errors.New("port_start must be less than or equal to port_end")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
