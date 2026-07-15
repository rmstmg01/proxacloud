package databaseclusters

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) emailDeploymentDetails(clusterID int64) (bool, error) {
	if s.mailer == nil || clusterID <= 0 {
		return false, nil
	}
	var email, firstName, lastName, username string
	var alreadySent bool
	err := s.db.QueryRow(`
		SELECT u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), u.username,
		       (d.details_emailed_at IS NOT NULL)
		  FROM database_clusters d
		  JOIN customer_accounts ca ON ca.id = d.customer_account_id
		  JOIN users u ON u.id = ca.owner_user_id
		 WHERE d.id = $1`, clusterID,
	).Scan(&email, &firstName, &lastName, &username, &alreadySent)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load database cluster customer email: %w", err)
	}
	if alreadySent || strings.TrimSpace(email) == "" {
		return false, nil
	}
	cluster, err := s.Get(clusterID)
	if err != nil {
		return false, err
	}
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		name = username
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Hello %s,\n\n== ProxaOps Cloud Database Cluster ==\n\nYour MariaDB Galera cluster is ready.\n\n", name)
	fmt.Fprintf(&body, "Name: %s\nRegion: %s\nMariaDB version: %s\nOperating system: %s\nDatabase: %s\nApplication user: %s\nEndpoint: %s.%s:3306\nGalera nodes: %d\n\n", cluster.Name, cluster.RegionName, cluster.MariaDBVersion, cluster.CloudInitImagePath, cluster.DatabaseName, cluster.AppUsername, cluster.HAProxyServiceName, cluster.HAProxyNamespace, len(cluster.Nodes))
	for _, node := range cluster.Nodes {
		fmt.Fprintf(&body, "%s\nPrivate IP: %s\n\n", node.VMName, node.IPAddress)
	}
	body.WriteString("Sign in to ProxaCloud to securely reveal or copy the database passwords. Passwords are not included in email.\n\n--\nProxaOps Cloud\n")
	if err := s.mailer.Send(strings.TrimSpace(email), fmt.Sprintf("Your ProxaCloud database cluster %s is ready", cluster.Name), body.String()); err != nil {
		return false, fmt.Errorf("send database cluster details email: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE database_clusters SET details_emailed_at = NOW(), updated_at = NOW() WHERE id = $1 AND details_emailed_at IS NULL`, clusterID); err != nil {
		return false, fmt.Errorf("record database cluster details email: %w", err)
	}
	return true, nil
}
