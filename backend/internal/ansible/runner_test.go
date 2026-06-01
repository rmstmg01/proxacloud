package ansible

import (
	"strings"
	"testing"
)

func TestCompactProgressOutputPreservesShortOutput(t *testing.T) {
	if got := compactProgressOutput("  TASK [example]  \n"); got != "TASK [example]" {
		t.Fatalf("unexpected compact output %q", got)
	}
}

func TestCompactProgressOutputKeepsLatestOutputWhenTruncated(t *testing.T) {
	input := strings.Repeat("a", maxProgressOutputLength) + "latest"
	got := compactProgressOutput(input)

	if !strings.HasPrefix(got, "[output truncated; showing latest updates]\n") {
		t.Fatalf("expected truncation notice, got %q", got)
	}
	if !strings.HasSuffix(got, "latest") {
		t.Fatalf("expected latest output to be retained, got %q", got)
	}
}

func TestRenderPlaybookConfiguresKubeVIPForBootstrapAndSecondaryControlPlanes(t *testing.T) {
	spec := BootstrapSpec{
		KubernetesVersion: "v1.31.0",
		ControlPlaneVIP:   "192.168.8.85",
		DeployKubeVIP:     true,
		Nodes: []Node{
			{Name: "cp-1", IP: "192.168.8.110", Role: "control-plane"},
			{Name: "cp-2", IP: "192.168.8.111", Role: "control-plane"},
			{Name: "worker-1", IP: "192.168.8.112", Role: "worker"},
		},
	}

	playbook, err := renderPlaybook(spec, "test-run")
	if err != nil {
		t.Fatalf("render playbook: %v", err)
	}

	for _, expected := range []string{
		"--control-plane-endpoint=192.168.8.85:6443",
		"Configure kube-vip Kubernetes client kubeconfig",
		"- name: k8s_config_file",
		"value: /etc/kubernetes/admin.conf",
		"Use super-admin kubeconfig for kube-vip during kubeadm initialization",
		"replace: '/etc/kubernetes/super-admin.conf'",
		"Use admin kubeconfig for kube-vip after kubeadm initialization",
		"regexp: '/etc/kubernetes/super-admin\\.conf'",
		"Generate kube-vip static pod manifest on secondary control plane",
	} {
		if !strings.Contains(playbook, expected) {
			t.Fatalf("expected rendered playbook to contain %q", expected)
		}
	}
}

func TestRenderPlaybookDoesNotConfigureKubeVIPWhenDisabled(t *testing.T) {
	spec := BootstrapSpec{
		KubernetesVersion: "v1.31.0",
		ControlPlaneVIP:   "192.168.8.85",
		DeployKubeVIP:     false,
		Nodes: []Node{
			{Name: "cp-1", IP: "192.168.8.110", Role: "control-plane"},
			{Name: "cp-2", IP: "192.168.8.111", Role: "control-plane"},
		},
	}

	playbook, err := renderPlaybook(spec, "test-run")
	if err != nil {
		t.Fatalf("render playbook: %v", err)
	}

	if strings.Contains(playbook, "kube-vip") {
		t.Fatal("expected rendered playbook not to configure kube-vip")
	}
	if !strings.Contains(playbook, "--control-plane-endpoint=192.168.8.110:6443") {
		t.Fatal("expected rendered playbook to use the first control-plane IP")
	}
}
