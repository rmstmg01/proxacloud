package clusters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HealthChecker validates node availability against Proxmox
type HealthChecker interface {
	CheckNodeHealth(credentialID int64, vmName string, proxmoxNodeName string) (string, error)
}

// ReconciliationWorker monitors cluster health and triggers recovery
type ReconciliationWorker struct {
	db             *sql.DB
	healthChecker  HealthChecker
	proxmoxSvc     ProxmoxCredentialStore
	clusterService *Service
	stopChan       chan struct{}
	stopped        bool
}

// NewReconciliationWorker creates a new reconciliation worker
func NewReconciliationWorker(db *sql.DB, healthChecker HealthChecker, proxmoxSvc ProxmoxCredentialStore) *ReconciliationWorker {
	return &ReconciliationWorker{
		db:            db,
		healthChecker: healthChecker,
		proxmoxSvc:    proxmoxSvc,
		stopChan:      make(chan struct{}),
	}
}

// SetService sets the cluster service for recovery operations
func (rw *ReconciliationWorker) SetService(svc *Service) {
	rw.clusterService = svc
}

// Start begins the reconciliation loop
func (rw *ReconciliationWorker) Start(checkInterval time.Duration) {
	if rw.stopped {
		rw.stopChan = make(chan struct{})
		rw.stopped = false
	}

	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		// Run one check immediately
		rw.reconcileAllClusters()

		for {
			select {
			case <-rw.stopChan:
				log.Println("[ReconciliationWorker] Stopping reconciliation loop")
				rw.stopped = true
				return
			case <-ticker.C:
				rw.reconcileAllClusters()
			}
		}
	}()
}

// Stop gracefully stops the reconciliation worker
func (rw *ReconciliationWorker) Stop() {
	if !rw.stopped {
		close(rw.stopChan)
	}
}

// reconcileAllClusters runs health checks on all clusters and recovery on clusters with auto-recovery enabled.
func (rw *ReconciliationWorker) reconcileAllClusters() {
	rows, err := rw.db.Query(`
		SELECT DISTINCT c.id
		FROM clusters c
		JOIN cluster_nodes cn ON cn.cluster_id = c.id
	`)
	if err != nil {
		log.Printf("[ReconciliationWorker] Error querying recovery configs: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var clusterID int64
		if err := rows.Scan(&clusterID); err != nil {
			log.Printf("[ReconciliationWorker] Error scanning cluster ID: %v", err)
			continue
		}

		rw.reconcileCluster(clusterID)
	}
}

// reconcileCluster performs health checks and recovery for a single cluster
func (rw *ReconciliationWorker) reconcileCluster(clusterID int64) {
	cfg, err := getRecoveryConfig(rw.db, clusterID)
	if err != nil {
		log.Printf("[ReconciliationWorker] Failed to load recovery config for cluster %d: %v", clusterID, err)
		return
	}

	// Get all nodes in the cluster
	nodes, err := getClusterNodesForReconciliation(rw.db, clusterID)
	if err != nil {
		log.Printf("[ReconciliationWorker] Failed to load cluster nodes for %d: %v", clusterID, err)
		return
	}

	for _, node := range nodes {
		rw.checkAndRecoverNode(clusterID, node, cfg)
	}

	kubernetesStatus := checkKubernetesNodeReadiness(rw.db, clusterID, nodes)
	if err := updateClusterAggregateHealthStatus(rw.db, clusterID, kubernetesStatus); err != nil {
		log.Printf("[ReconciliationWorker] Failed to update aggregate health for cluster %d: %v", clusterID, err)
	}
}

// checkAndRecoverNode checks health of a single node and triggers recovery if needed
func (rw *ReconciliationWorker) checkAndRecoverNode(clusterID int64, node clusterNodeForReconciliation, cfg ClusterRecoveryConfig) {
	// Check health against Proxmox
	healthStatus, checkErr := rw.healthChecker.CheckNodeHealth(node.ProxmoxCredentialID, node.VMName, node.ProxmoxNodeName)

	// Update health record
	if err := updateNodeHealthStatus(rw.db, clusterID, node.ClusterNodeID, node.VMName, node.Role, healthStatus); err != nil {
		log.Printf("[ReconciliationWorker] Failed to update health status for node %s: %v", node.VMName, err)
	}

	if (healthStatus == "unhealthy" || healthStatus == "missing") && cfg.AutoRecoveryEnabled {
		// Increment failure count
		failures, err := incrementNodeFailureCount(rw.db, clusterID, node.ClusterNodeID)
		if err != nil {
			log.Printf("[ReconciliationWorker] Failed to increment failure count for node %s: %v", node.VMName, err)
			return
		}

		// Check if threshold exceeded
		if failures >= cfg.FailureThresholdCount {
			log.Printf("[ReconciliationWorker] Node %s exceeded failure threshold (%d/%d), triggering recovery", node.VMName, failures, cfg.FailureThresholdCount)

			// Trigger automatic recovery
			if err := rw.triggerAutoRecovery(clusterID, node); err != nil {
				logRecoveryEvent(rw.db, clusterID, sql.NullInt64{Int64: node.ClusterNodeID, Valid: true}, "recovery_failed", node.VMName, node.Role, "failed", fmt.Sprintf("Auto-recovery failed: %v", err))
				log.Printf("[ReconciliationWorker] Auto-recovery failed for node %s: %v", node.VMName, err)
			} else {
				logRecoveryEvent(rw.db, clusterID, sql.NullInt64{Int64: node.ClusterNodeID, Valid: true}, "recovery_triggered", node.VMName, node.Role, "in-progress", "Automatic recovery initiated")
			}
		}
	} else if healthStatus == "healthy" && checkErr == nil {
		// Node is healthy, reset failure count
		if err := resetNodeFailureCount(rw.db, clusterID, node.ClusterNodeID); err != nil {
			log.Printf("[ReconciliationWorker] Failed to reset failure count for node %s: %v", node.VMName, err)
		}
	}
}

// triggerAutoRecovery automatically replaces a failed node
func (rw *ReconciliationWorker) triggerAutoRecovery(clusterID int64, node clusterNodeForReconciliation) error {
	if rw.clusterService == nil {
		return fmt.Errorf("cluster service not initialized for auto-recovery")
	}

	// For worker nodes, select a new Proxmox node
	var newProxmoxNode string
	if node.Role == "worker" {
		// Select a different Proxmox node (round-robin strategy)
		selectedNode, err := selectAlternateProxmoxNode(rw.db, clusterID, node.ProxmoxNodeName)
		if err != nil {
			return fmt.Errorf("failed to select alternate proxmox node: %w", err)
		}
		newProxmoxNode = selectedNode
	} else if node.Role == "control-plane" {
		// For control plane nodes, use the same Proxmox node (assumes VM was deleted, not node failure)
		newProxmoxNode = node.ProxmoxNodeName
	} else {
		return fmt.Errorf("unsupported role for auto-recovery: %s", node.Role)
	}

	// Create a ReplaceNodeRequest
	req := ReplaceNodeRequest{
		FailedVMName:       node.VMName,
		NewProxmoxNodeName: newProxmoxNode,
		NewIPAddress:       "", // Let it auto-assign
	}

	// Trigger the actual ReplaceNode operation
	resp, err := rw.clusterService.ReplaceNode(clusterID, req)
	if err != nil {
		return fmt.Errorf("replace node failed: %w", err)
	}

	log.Printf("[ReconciliationWorker] Successfully triggered auto-recovery for %s -> %s", node.VMName, resp.NewVMName)
	return nil
}

// Helper functions for database operations

type clusterNodeForReconciliation struct {
	ClusterNodeID       int64
	VMName              string
	Role                string
	ProxmoxNodeName     string
	ProxmoxCredentialID int64
	IPAddress           string
}

func getClusterNodesForReconciliation(db *sql.DB, clusterID int64) ([]clusterNodeForReconciliation, error) {
	rows, err := db.Query(`
		SELECT cn.id, cn.vm_name, cn.role, cn.proxmox_node_name, COALESCE(cn.proxmox_credential_id, 0), COALESCE(cn.ip_address, '')
		FROM cluster_nodes cn
		WHERE cn.cluster_id = $1
		ORDER BY cn.role DESC, cn.role_index
	`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []clusterNodeForReconciliation
	for rows.Next() {
		var node clusterNodeForReconciliation
		if err := rows.Scan(&node.ClusterNodeID, &node.VMName, &node.Role, &node.ProxmoxNodeName, &node.ProxmoxCredentialID, &node.IPAddress); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

func getRecoveryConfig(db *sql.DB, clusterID int64) (ClusterRecoveryConfig, error) {
	var cfg ClusterRecoveryConfig
	err := db.QueryRow(`
		SELECT cluster_id, auto_recovery_enabled, health_check_interval_seconds, failure_threshold_count
		FROM cluster_recovery_config
		WHERE cluster_id = $1
	`, clusterID).Scan(&cfg.ClusterID, &cfg.AutoRecoveryEnabled, &cfg.HealthCheckInterval, &cfg.FailureThresholdCount)

	if errors.Is(err, sql.ErrNoRows) {
		// Return default config if not found
		return ClusterRecoveryConfig{
			ClusterID:             clusterID,
			AutoRecoveryEnabled:   false,
			HealthCheckInterval:   30,
			FailureThresholdCount: 3,
		}, nil
	}

	return cfg, err
}

func updateNodeHealthStatus(db *sql.DB, clusterID int64, clusterNodeID int64, vmName string, role string, healthStatus string) error {
	status := normalizeNodeHealthStatus(healthStatus)

	_, err := db.Exec(`
		INSERT INTO cluster_node_health (cluster_id, cluster_node_id, vm_name, role, health_status, last_check_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (cluster_id, cluster_node_id) DO UPDATE
		SET health_status = EXCLUDED.health_status, last_check_at = NOW(), updated_at = NOW()
	`, clusterID, clusterNodeID, vmName, role, status)

	return err
}

func updateClusterAggregateHealthStatus(db *sql.DB, clusterID int64, kubernetesStatus string) error {
	var healthyCount, unhealthyCount, missingCount, unknownCount int
	var lifecycleStatus string
	err := db.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE health_status = 'healthy'),
			COUNT(*) FILTER (WHERE health_status = 'unhealthy'),
			COUNT(*) FILTER (WHERE health_status = 'missing'),
			COUNT(*) FILTER (WHERE health_status = 'unknown')
		FROM cluster_node_health
		WHERE cluster_id = $1
	`, clusterID).Scan(&healthyCount, &unhealthyCount, &missingCount, &unknownCount)
	if err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT status FROM clusters WHERE id = $1`, clusterID).Scan(&lifecycleStatus); err != nil {
		return err
	}

	status := aggregateHealthStatus(healthyCount, unhealthyCount, missingCount, unknownCount, lifecycleStatus, kubernetesStatus)
	_, err = db.Exec(`
		UPDATE clusters
		SET health_status = $2, health_checked_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, clusterID, status)
	return err
}

func aggregateHealthStatus(healthyCount, unhealthyCount, missingCount, unknownCount int, lifecycleStatus string, kubernetesStatus string) string {
	total := healthyCount + unhealthyCount + missingCount + unknownCount
	if total == 0 {
		return "unknown"
	}
	if strings.EqualFold(lifecycleStatus, "offline") && healthyCount == 0 && missingCount == 0 && unhealthyCount > 0 {
		return "offline"
	}
	if missingCount > 0 || (unhealthyCount > 0 && healthyCount == 0) {
		return "critical"
	}
	if unhealthyCount > 0 || unknownCount > 0 {
		return "degraded"
	}
	switch normalizeClusterHealthStatus(kubernetesStatus) {
	case "healthy":
		return "healthy"
	case "critical":
		return "critical"
	case "degraded", "unknown":
		return "degraded"
	}
	return "healthy"
}

func checkKubernetesNodeReadiness(db *sql.DB, clusterID int64, nodes []clusterNodeForReconciliation) string {
	kubeconfigPath, err := resolveClusterHealthKubeconfigPath(db, clusterID)
	if err != nil {
		log.Printf("[ReconciliationWorker] Kubernetes readiness skipped for cluster %d: %v", clusterID, err)
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "nodes", "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[ReconciliationWorker] Kubernetes readiness check failed for cluster %d: %v; output: %s", clusterID, err, strings.TrimSpace(string(output)))
		return "degraded"
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		log.Printf("[ReconciliationWorker] Kubernetes readiness response decode failed for cluster %d: %v", clusterID, err)
		return "degraded"
	}

	readyByName := make(map[string]bool, len(payload.Items))
	for _, item := range payload.Items {
		name := strings.ToLower(strings.TrimSpace(item.Metadata.Name))
		if name == "" {
			continue
		}
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				readyByName[name] = true
				break
			}
		}
		if _, ok := readyByName[name]; !ok {
			readyByName[name] = false
		}
	}
	if len(readyByName) == 0 {
		return "degraded"
	}

	if len(nodes) > 0 {
		for _, node := range nodes {
			vmName := strings.ToLower(strings.TrimSpace(node.VMName))
			if vmName == "" {
				continue
			}
			if !readyByName[vmName] {
				return "degraded"
			}
		}
	} else {
		for _, ready := range readyByName {
			if !ready {
				return "degraded"
			}
		}
	}

	return "healthy"
}

func resolveClusterHealthKubeconfigPath(db *sql.DB, clusterID int64) (string, error) {
	var workDir string
	err := db.QueryRow(`
		SELECT work_dir
		FROM provision_runs
		WHERE cluster_id = $1 AND action = 'bootstrap' AND work_dir <> ''
		ORDER BY created_at DESC
		LIMIT 1
	`, clusterID).Scan(&workDir)
	if err == nil {
		candidate := filepath.Join(workDir, "admin.conf")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	if envPath := strings.TrimSpace(os.Getenv("KUBECONFIG")); envPath != "" {
		return resolveHealthKubeconfigFile(envPath)
	}
	return resolveHealthKubeconfigFile("/app/backend/.kube/config")
}

func resolveHealthKubeconfigFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	candidate := filepath.Join(path, "config")
	info, err = os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("kubeconfig path %s is a directory", candidate)
	}
	return candidate, nil
}

func normalizeNodeHealthStatus(status string) string {
	switch status {
	case "healthy", "unhealthy", "missing", "unknown":
		return status
	default:
		return "unknown"
	}
}

func normalizeClusterHealthStatus(status string) string {
	switch status {
	case "healthy", "degraded", "critical", "offline", "unknown":
		return status
	default:
		return "unknown"
	}
}

func healthStatusForPowerAction(action string) string {
	switch action {
	case "start", "restart":
		return "healthy"
	case "shutdown":
		return "offline"
	default:
		return "unknown"
	}
}

func incrementNodeFailureCount(db *sql.DB, clusterID int64, clusterNodeID int64) (int, error) {
	var failures int
	err := db.QueryRow(`
		UPDATE cluster_node_health
		SET consecutive_failures = consecutive_failures + 1, updated_at = NOW()
		WHERE cluster_id = $1 AND cluster_node_id = $2
		RETURNING consecutive_failures
	`, clusterID, clusterNodeID).Scan(&failures)

	return failures, err
}

func resetNodeFailureCount(db *sql.DB, clusterID int64, clusterNodeID int64) error {
	_, err := db.Exec(`
		UPDATE cluster_node_health
		SET consecutive_failures = 0, updated_at = NOW()
		WHERE cluster_id = $1 AND cluster_node_id = $2
	`, clusterID, clusterNodeID)

	return err
}

func selectAlternateProxmoxNode(db *sql.DB, clusterID int64, currentNode string) (string, error) {
	var alternateNode string
	err := db.QueryRow(`
		SELECT DISTINCT proxmox_node_name
		FROM cluster_nodes
		WHERE cluster_id = $1 AND proxmox_node_name != $2
		LIMIT 1
	`, clusterID, currentNode).Scan(&alternateNode)

	if errors.Is(err, sql.ErrNoRows) {
		// If no alternate, use the current one
		return currentNode, nil
	}

	return alternateNode, err
}

func logRecoveryEvent(db *sql.DB, clusterID int64, clusterNodeID sql.NullInt64, eventType string, vmName string, role string, status string, message string) error {
	_, err := db.Exec(`
		INSERT INTO cluster_recovery_events (cluster_id, cluster_node_id, event_type, vm_name, role, status, message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, clusterID, clusterNodeID, eventType, vmName, role, status, message)

	return err
}

// GetClusterHealth retrieves the overall health status of a cluster
func (s *Service) GetClusterHealth(clusterID int64) (ClusterHealthResponse, error) {
	// Verify cluster exists
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ClusterHealthResponse{}, err
	}

	// Get health records for all nodes
	rows, err := s.db.Query(`
		SELECT cnh.cluster_node_id, cnh.vm_name, cnh.role, cnh.health_status, cnh.last_check_at, cnh.consecutive_failures
		FROM cluster_node_health cnh
		WHERE cnh.cluster_id = $1
		ORDER BY cnh.role DESC, cnh.vm_name
	`, clusterID)
	if err != nil {
		return ClusterHealthResponse{}, fmt.Errorf("query cluster health: %w", err)
	}
	defer rows.Close()

	response := ClusterHealthResponse{
		ClusterID:    clusterID,
		NodeStatuses: []NodeHealthStatus{},
		LastCheckAt:  time.Now(),
	}

	var healthyCount, unhealthyCount, missingCount, unknownCount int

	for rows.Next() {
		var nodeID int64
		var vmName, role, healthStatus string
		var lastCheck time.Time
		var failures int

		if err := rows.Scan(&nodeID, &vmName, &role, &healthStatus, &lastCheck, &failures); err != nil {
			return ClusterHealthResponse{}, err
		}

		recommendedAction := "none"
		if healthStatus == "unhealthy" {
			recommendedAction = "replace"
		} else if healthStatus == "missing" {
			recommendedAction = "immediate-replace"
		}

		nodeStatus := NodeHealthStatus{
			ClusterID:           clusterID,
			NodeID:              nodeID,
			VMName:              vmName,
			Role:                role,
			HealthStatus:        healthStatus,
			LastCheckAt:         lastCheck,
			ConsecutiveFailures: failures,
			RecommendedAction:   recommendedAction,
		}

		// Check if auto-recovery is enabled and threshold exceeded
		if cfg, err := getRecoveryConfig(s.db, clusterID); err == nil && cfg.AutoRecoveryEnabled {
			nodeStatus.RecoveryEligible = failures >= cfg.FailureThresholdCount
		}

		response.NodeStatuses = append(response.NodeStatuses, nodeStatus)

		switch healthStatus {
		case "healthy":
			healthyCount++
		case "unhealthy":
			unhealthyCount++
		case "missing":
			missingCount++
		case "unknown":
			unknownCount++
		}

		if lastCheck.After(response.LastCheckAt) || response.LastCheckAt.IsZero() {
			response.LastCheckAt = lastCheck
		}
	}

	response.HealthyNodes = healthyCount
	response.UnhealthyNodes = unhealthyCount
	response.MissingNodes = missingCount

	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return ClusterHealthResponse{}, err
	}
	kubernetesStatus := checkKubernetesNodeReadiness(s.db, clusterID, nil)
	response.OverallStatus = aggregateHealthStatus(healthyCount, unhealthyCount, missingCount, unknownCount, cluster.Status, kubernetesStatus)

	return response, nil
}

// SetRecoveryConfig enables or disables auto-recovery for a cluster
func (s *Service) SetRecoveryConfig(clusterID int64, req ClusterRecoveryConfig) error {
	// Verify cluster exists
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return err
	}

	// Validate intervals
	if req.HealthCheckInterval < 10 {
		return errors.New("health_check_interval_seconds must be at least 10")
	}
	if req.FailureThresholdCount < 1 {
		return errors.New("failure_threshold_count must be at least 1")
	}

	_, err := s.db.Exec(`
		INSERT INTO cluster_recovery_config (cluster_id, auto_recovery_enabled, health_check_interval_seconds, failure_threshold_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (cluster_id) DO UPDATE
		SET auto_recovery_enabled = EXCLUDED.auto_recovery_enabled,
		    health_check_interval_seconds = EXCLUDED.health_check_interval_seconds,
		    failure_threshold_count = EXCLUDED.failure_threshold_count,
		    updated_at = NOW()
	`, clusterID, req.AutoRecoveryEnabled, req.HealthCheckInterval, req.FailureThresholdCount)

	return err
}

// InitializeClusterHealth creates initial health records for all nodes in a cluster
func (s *Service) InitializeClusterHealth(clusterID int64) error {
	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		status := "unknown"
		_, err := s.db.Exec(`
			INSERT INTO cluster_node_health (cluster_id, cluster_node_id, vm_name, role, health_status, last_check_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW(), NOW())
			ON CONFLICT (cluster_id, cluster_node_id) DO NOTHING
		`, clusterID, node.ID, node.VMName, node.Role, status)

		if err != nil {
			log.Printf("Warning: Failed to initialize health record for node %s: %v", node.VMName, err)
		}
	}

	return nil
}
