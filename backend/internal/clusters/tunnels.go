package clusters

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"proxacloud/backend/internal/tunnelinfra"
)

func (s *Service) SetTunnelInfrastructure(infra *tunnelinfra.Service, portStart, portEnd int) {
	s.tunnelInfra = infra
	if portStart <= 0 {
		portStart = 22000
	}
	if portEnd < portStart {
		portEnd = 29999
	}
	s.tunnelPortStart, s.tunnelPortEnd = portStart, portEnd
}

func (s *Service) provisionClusterNodeTunnel(clusterNodeID, clusterID int64, vmName string) (string, string) {
	if s.tunnelInfra == nil || !s.tunnelInfra.Enabled() {
		return "", ""
	}
	hostname, err := s.tunnelInfra.Hostname(vmName)
	if err != nil {
		return "", "tunnel hostname: " + err.Error()
	}
	_, _ = s.db.Exec(`UPDATE cluster_nodes SET fqdn = $2 WHERE id = $1`, clusterNodeID, hostname)
	ports, err := s.clusterTunnelPortCandidates()
	if err != nil {
		return "", "allocate tunnel port: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result, err := s.tunnelInfra.Provision(ctx, tunnelinfra.ProvisionRequest{
		Name: fmt.Sprintf("Cluster node %s", vmName), Slug: "cluster-node-" + vmName,
		Description: fmt.Sprintf("Managed SSH tunnel for cluster %d node %s", clusterID, vmName),
		Hostname:    hostname, PublicPorts: ports, TCPTargetHost: "127.0.0.1", TCPTargetPort: 22,
		CreateHTTPRoute: false, TokenName: "Cluster node agent token", TCPRouteName: "SSH",
	})
	if err != nil {
		_, _ = s.db.Exec(`INSERT INTO cluster_node_tunnel_attachments (cluster_node_id, public_hostname, public_port, status, last_error) VALUES ($1,$2,$3,'failed',$4) ON CONFLICT (cluster_node_id) DO UPDATE SET status='failed', last_error=EXCLUDED.last_error, updated_at=NOW()`, clusterNodeID, hostname, ports[0], err.Error())
		return "", fmt.Sprintf("tunnel setup failed for %s: %v", vmName, err)
	}
	redacted := result.InstallCommand
	if marker := strings.Index(redacted, "--token"); marker >= 0 {
		redacted = redacted[:marker] + "--token 'REDACTED'"
	}
	_, err = s.db.Exec(`INSERT INTO cluster_node_tunnel_attachments (cluster_node_id,tunnel_id,http_route_id,tcp_route_id,token_id,public_hostname,public_port,cloudflare_dns_record_id,cloudflare_dns_record_type,cloudflare_dns_record_content,cloudflare_dns_proxied,cloudflare_dns_status,status,last_error) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'configured','') ON CONFLICT (cluster_node_id) DO UPDATE SET tunnel_id=EXCLUDED.tunnel_id,http_route_id=EXCLUDED.http_route_id,tcp_route_id=EXCLUDED.tcp_route_id,token_id=EXCLUDED.token_id,public_hostname=EXCLUDED.public_hostname,public_port=EXCLUDED.public_port,cloudflare_dns_record_id=EXCLUDED.cloudflare_dns_record_id,cloudflare_dns_record_type=EXCLUDED.cloudflare_dns_record_type,cloudflare_dns_record_content=EXCLUDED.cloudflare_dns_record_content,cloudflare_dns_proxied=EXCLUDED.cloudflare_dns_proxied,cloudflare_dns_status=EXCLUDED.cloudflare_dns_status,status='configured',last_error='',updated_at=NOW()`, clusterNodeID, result.TunnelID, result.HTTPRouteID, result.TCPRouteID, result.TokenID, result.PublicHostname, result.PublicPort, result.DNS.RecordID, result.DNS.RecordType, result.DNS.Content, result.DNS.Proxied, result.DNS.Status)
	if err != nil {
		_ = s.tunnelInfra.Cleanup(ctx, result)
		return "", fmt.Sprintf("save tunnel metadata for %s: %v", vmName, err)
	}
	_ = redacted // intentionally never persisted with the live token
	return result.InstallCommand, ""
}

func (s *Service) clusterTunnelPortCandidates() ([]int, error) {
	rows, err := s.db.Query(`SELECT public_port FROM vps_tunnel_attachments WHERE public_port BETWEEN $1 AND $2 UNION SELECT public_port FROM cluster_node_tunnel_attachments WHERE public_port BETWEEN $1 AND $2`, s.tunnelPortStart, s.tunnelPortEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var port int
		if rows.Scan(&port) == nil {
			used[port] = true
		}
	}
	out := make([]int, 0, s.tunnelPortEnd-s.tunnelPortStart+1)
	for port := s.tunnelPortStart; port <= s.tunnelPortEnd; port++ {
		if !used[port] {
			out = append(out, port)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tunnel ports available between %d and %d", s.tunnelPortStart, s.tunnelPortEnd)
	}
	return out, nil
}

func (s *Service) cleanupClusterTunnels(clusterID int64) error {
	if s.tunnelInfra == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT a.tunnel_id,a.http_route_id,a.tcp_route_id,a.token_id,a.public_hostname,a.public_port,a.cloudflare_dns_record_id,a.cloudflare_dns_record_type,a.cloudflare_dns_record_content,a.cloudflare_dns_proxied,a.cloudflare_dns_status FROM cluster_node_tunnel_attachments a JOIN cluster_nodes n ON n.id=a.cluster_node_id WHERE n.cluster_id=$1`, clusterID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r tunnelinfra.Result
		if err := rows.Scan(&r.TunnelID, &r.HTTPRouteID, &r.TCPRouteID, &r.TokenID, &r.PublicHostname, &r.PublicPort, &r.DNS.RecordID, &r.DNS.RecordType, &r.DNS.Content, &r.DNS.Proxied, &r.DNS.Status); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := s.tunnelInfra.Cleanup(ctx, r)
		cancel()
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) cleanupClusterNodeTunnel(clusterNodeID int64) error {
	if s.tunnelInfra == nil {
		return nil
	}
	var r tunnelinfra.Result
	err := s.db.QueryRow(`SELECT tunnel_id,http_route_id,tcp_route_id,token_id,public_hostname,public_port,cloudflare_dns_record_id,cloudflare_dns_record_type,cloudflare_dns_record_content,cloudflare_dns_proxied,cloudflare_dns_status FROM cluster_node_tunnel_attachments WHERE cluster_node_id=$1`, clusterNodeID).Scan(&r.TunnelID, &r.HTTPRouteID, &r.TCPRouteID, &r.TokenID, &r.PublicHostname, &r.PublicPort, &r.DNS.RecordID, &r.DNS.RecordType, &r.DNS.Content, &r.DNS.Proxied, &r.DNS.Status)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.tunnelInfra.Cleanup(ctx, r)
}
