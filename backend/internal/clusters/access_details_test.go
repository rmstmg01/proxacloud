package clusters

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildClusterAccessDetailsReturnsCustomerSafeNodeAccess(t *testing.T) {
	details := buildClusterAccessDetails(ClusterSummary{
		ID: 99, Name: "sulikot", KubernetesVersion: "v1.31.4", TopologyType: "tri", Status: "bootstrapped",
	}, []ClusterNodeRecord{{
		Role: "control-plane", RoleIndex: 1, VMName: "sulikot-cp-1", IPAddress: "10.0.0.21",
		Hostname: "sulikot-cp-1", FQDN: "sulikot-cp-1.proxaops.com", TunnelPublicPort: 22001,
		SSHCommand:      "ssh root@sulikot-cp-1.proxaops.com -p 22001",
		ProxmoxNodeName: "pve01", ProxmoxCredentialID: 42,
	}}, "10.0.0.20")

	if details.APIEndpoint != "https://10.0.0.20:6443" {
		t.Fatalf("APIEndpoint = %q", details.APIEndpoint)
	}
	if len(details.Nodes) != 1 || details.Nodes[0].FQDN != "sulikot-cp-1.proxaops.com" {
		t.Fatalf("unexpected access nodes: %#v", details.Nodes)
	}
	if details.Nodes[0].SSHCommand != "ssh root@sulikot-cp-1.proxaops.com -p 22001" {
		t.Fatalf("SSHCommand = %q", details.Nodes[0].SSHCommand)
	}

	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	for _, internalField := range []string{"proxmox_node_name", "proxmox_credential_id", "kubeconfig_path", "work_dir"} {
		if strings.Contains(string(raw), internalField) {
			t.Fatalf("customer access response contains internal field %q: %s", internalField, raw)
		}
	}
}

func TestBuildClusterAccessDetailsFallsBackToControlPlaneIP(t *testing.T) {
	details := buildClusterAccessDetails(ClusterSummary{ID: 7}, []ClusterNodeRecord{
		{Role: "worker", IPAddress: "10.0.0.31"},
		{Role: "control-plane", IPAddress: "10.0.0.21"},
	}, "")
	if details.APIEndpoint != "https://10.0.0.21:6443" {
		t.Fatalf("APIEndpoint = %q", details.APIEndpoint)
	}
}
