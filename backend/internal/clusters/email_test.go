package clusters

import (
	"strings"
	"testing"
)

func TestBuildClusterDeploymentEmail(t *testing.T) {
	body := buildClusterDeploymentEmail(clusterDeploymentEmailData{
		CustomerName:      "Jane Doe",
		ClusterName:       "production",
		KubernetesVersion: "v1.31.4",
		TopologyType:      "tri",
		ControlPlaneCount: 3,
		WorkerCount:       2,
		ControlPlaneVIP:   "10.0.0.20",
		Nodes: []ClusterNodeRecord{{
			VMName: "production-cp-1", Role: "control-plane", IPAddress: "10.0.0.21",
			FQDN: "production-cp-1.proxaops.com", SSHCommand: "ssh root@production-cp-1.proxaops.com -p 22001",
		}},
	})
	for _, expected := range []string{
		"Hello Jane Doe", "production", "v1.31.4", "https://10.0.0.20:6443",
		"production-cp-1.proxaops.com", "ssh root@production-cp-1.proxaops.com -p 22001",
		"download its kubeconfig",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("deployment email missing %q: %s", expected, body)
		}
	}
	for _, secret := range []string{"private_key", "client-key-data", "token"} {
		if strings.Contains(strings.ToLower(body), secret) {
			t.Fatalf("deployment email unexpectedly contains secret marker %q", secret)
		}
	}
}
