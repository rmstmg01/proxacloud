package clusters

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type clusterDeploymentEmailData struct {
	CustomerName      string
	ClusterName       string
	KubernetesVersion string
	TopologyType      string
	ControlPlaneCount int
	WorkerCount       int
	ControlPlaneVIP   string
	Nodes             []ClusterNodeRecord
}

func (s *Service) SetMailer(sender ClusterMailer) {
	s.mailer = sender
}

func (s *Service) emailDeploymentDetails(clusterID int64) (bool, error) {
	if s.mailer == nil || clusterID <= 0 {
		return false, nil
	}

	var email, firstName, lastName, username string
	var alreadyEmailed bool
	err := s.db.QueryRow(`
		SELECT u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), u.username,
		       (c.details_emailed_at IS NOT NULL)
		FROM clusters c
		JOIN customer_accounts ca ON ca.id = c.customer_account_id
		JOIN users u ON u.id = ca.owner_user_id
		WHERE c.id = $1
	`, clusterID).Scan(&email, &firstName, &lastName, &username, &alreadyEmailed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cluster customer email: %w", err)
	}
	if alreadyEmailed || strings.TrimSpace(email) == "" {
		return false, nil
	}

	summary, err := s.getClusterSummary(clusterID)
	if err != nil {
		return false, err
	}
	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return false, err
	}
	controlPlaneVIP := ""
	if bootstrap, bootstrapErr := s.getBootstrapConfig(clusterID); bootstrapErr == nil {
		controlPlaneVIP = strings.TrimSpace(bootstrap.ControlPlaneVIP)
	}
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		name = username
	}

	subject := fmt.Sprintf("Your ProxaCloud Kubernetes cluster %s is ready", summary.Name)
	body := buildClusterDeploymentEmail(clusterDeploymentEmailData{
		CustomerName:      name,
		ClusterName:       summary.Name,
		KubernetesVersion: summary.KubernetesVersion,
		TopologyType:      summary.TopologyType,
		ControlPlaneCount: summary.ControlPlaneCount,
		WorkerCount:       summary.WorkerCount,
		ControlPlaneVIP:   controlPlaneVIP,
		Nodes:             nodes,
	})
	if err := s.mailer.Send(strings.TrimSpace(email), subject, body); err != nil {
		return false, fmt.Errorf("send cluster deployment details email: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE clusters SET details_emailed_at = NOW(), updated_at = NOW() WHERE id = $1 AND details_emailed_at IS NULL`, clusterID); err != nil {
		return false, fmt.Errorf("record cluster deployment details email: %w", err)
	}
	return true, nil
}

func buildClusterDeploymentEmail(data clusterDeploymentEmailData) string {
	endpoint := strings.TrimSpace(data.ControlPlaneVIP)
	if endpoint == "" {
		for _, node := range data.Nodes {
			if node.Role == "control-plane" && strings.TrimSpace(node.IPAddress) != "" {
				endpoint = strings.TrimSpace(node.IPAddress)
				break
			}
		}
	}
	apiEndpoint := "Available through the downloaded kubeconfig"
	if endpoint != "" {
		apiEndpoint = fmt.Sprintf("https://%s:6443", endpoint)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "Hello %s,\n\n== ProxaOps Cloud Kubernetes Cluster ==\n\nYour Kubernetes cluster is ready.\n\n", data.CustomerName)
	fmt.Fprintf(&body, "Cluster details\nName: %s\nKubernetes version: %s\nTopology: %s\nControl-plane nodes: %d\nWorker nodes: %d\nKubernetes API: %s\n\n", data.ClusterName, data.KubernetesVersion, data.TopologyType, data.ControlPlaneCount, data.WorkerCount, apiEndpoint)
	body.WriteString("Node access\nSSH user: root\nAuthentication: Your selected SSH public key\n\n")
	for _, node := range data.Nodes {
		role := strings.ReplaceAll(node.Role, "-", " ")
		fmt.Fprintf(&body, "%s (%s)\n", node.VMName, role)
		if strings.TrimSpace(node.FQDN) != "" {
			fmt.Fprintf(&body, "Hostname: %s\n", node.FQDN)
		}
		if strings.TrimSpace(node.IPAddress) != "" {
			fmt.Fprintf(&body, "Private IP: %s\n", node.IPAddress)
		}
		if strings.TrimSpace(node.SSHCommand) != "" {
			fmt.Fprintf(&body, "SSH: %s\n", node.SSHCommand)
		}
		body.WriteString("\n")
	}
	body.WriteString("Sign in to ProxaCloud to view the cluster and securely download its kubeconfig.\n\nKeep your SSH private key and kubeconfig secure.\n\n--\nProxaOps Cloud\n")
	return body.String()
}
