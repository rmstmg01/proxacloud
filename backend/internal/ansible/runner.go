package ansible

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type Node struct {
	Name string
	IP   string
	Role string
}

type BootstrapSpec struct {
	ClusterName       string
	KubernetesVersion string
	SSHSUser          string
	SSHPrivateKey     string
	ControlPlaneVIP   string
	PodCIDR           string
	ServiceCIDR       string
	CNI               string
	Nodes             []Node
}

type RunResult struct {
	WorkDir string `json:"work_dir"`
	Output  string `json:"output"`
	Status  string `json:"status"`
}

type Runner struct {
	binary  string
	baseDir string
}

func NewRunner(binary, baseDir string) *Runner {
	if strings.TrimSpace(binary) == "" {
		binary = "ansible-playbook"
	}
	if strings.TrimSpace(baseDir) == "" {
		baseDir = filepath.Join("infra", "ansible", "runs")
	}
	return &Runner{binary: binary, baseDir: baseDir}
}

func (r *Runner) PrepareWorkspace(spec BootstrapSpec) (string, error) {
	if strings.TrimSpace(spec.ClusterName) == "" {
		return "", errors.New("cluster name is required")
	}
	if strings.TrimSpace(spec.KubernetesVersion) == "" {
		return "", errors.New("kubernetes version is required")
	}
	if strings.TrimSpace(spec.SSHSUser) == "" {
		return "", errors.New("ssh_user is required")
	}
	if strings.TrimSpace(spec.SSHPrivateKey) == "" {
		return "", errors.New("ssh_private_key is required")
	}
	if len(spec.Nodes) == 0 {
		return "", errors.New("at least one node is required")
	}

	workDir := filepath.Join(r.baseDir, sanitizeName(spec.ClusterName))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("create ansible workdir: %w", err)
	}

	keyPath := filepath.Join(workDir, "id_bootstrap")
	keyBody := strings.TrimSpace(spec.SSHPrivateKey) + "\n"
	if err := os.WriteFile(keyPath, []byte(keyBody), 0o600); err != nil {
		return "", fmt.Errorf("write bootstrap ssh key: %w", err)
	}

	inventory, err := renderInventory(spec, keyPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "inventory.ini"), []byte(inventory), 0o644); err != nil {
		return "", fmt.Errorf("write inventory: %w", err)
	}

	playbook, err := renderPlaybook(spec)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "bootstrap-k8s.yml"), []byte(playbook), 0o644); err != nil {
		return "", fmt.Errorf("write playbook: %w", err)
	}

	return workDir, nil
}

func (r *Runner) Apply(workDir string) (RunResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binary, "-i", "inventory.ini", "bootstrap-k8s.yml")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := stdout.String() + stderr.String()
	if err != nil {
		trimmed := strings.TrimSpace(combined)
		if trimmed == "" {
			return RunResult{WorkDir: workDir, Output: combined, Status: "failed"}, fmt.Errorf("run ansible playbook: %w", err)
		}
		return RunResult{WorkDir: workDir, Output: combined, Status: "failed"}, fmt.Errorf("run ansible playbook: %w\nansible output:\n%s", err, trimmed)
	}

	return RunResult{WorkDir: workDir, Output: combined, Status: "bootstrapped"}, nil
}

func renderInventory(spec BootstrapSpec, keyPath string) (string, error) {
	tpl, err := template.New("inventory").Parse(inventoryTemplate)
	if err != nil {
		return "", fmt.Errorf("parse inventory template: %w", err)
	}

	controlPlanes := make([]Node, 0)
	workers := make([]Node, 0)
	for _, n := range spec.Nodes {
		if n.Role == "control-plane" {
			controlPlanes = append(controlPlanes, n)
		} else {
			workers = append(workers, n)
		}
	}

	data := map[string]any{
		"ControlPlanes":     controlPlanes,
		"Workers":           workers,
		"SSHUser":           spec.SSHSUser,
		"SSHPrivateKeyPath": keyPath,
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render inventory template: %w", err)
	}
	return out.String(), nil
}

func renderPlaybook(spec BootstrapSpec) (string, error) {
	tpl, err := template.New("playbook").Parse(playbookTemplate)
	if err != nil {
		return "", fmt.Errorf("parse playbook template: %w", err)
	}

	cni := strings.TrimSpace(spec.CNI)
	if cni == "" {
		cni = "calico"
	}

	payload := map[string]any{
		"KubernetesVersion":    strings.TrimSpace(spec.KubernetesVersion),
		"KubernetesAptChannel": kubernetesAptChannel(spec.KubernetesVersion),
		"ControlPlaneVIP":      strings.TrimSpace(spec.ControlPlaneVIP),
		"PodCIDR":              nonEmpty(spec.PodCIDR, "10.244.0.0/16"),
		"ServiceCIDR":          nonEmpty(spec.ServiceCIDR, "10.96.0.0/12"),
		"CNI":                  cni,
		"InitEndpointArg":      initEndpointArg(spec.ControlPlaneVIP),
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, payload); err != nil {
		return "", fmt.Errorf("render playbook template: %w", err)
	}
	return out.String(), nil
}

func nonEmpty(input, fallback string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(name)))
}

func initEndpointArg(vip string) string {
	trimmed := strings.TrimSpace(vip)
	if trimmed == "" {
		return ""
	}
	return " --control-plane-endpoint=" + trimmed + ":6443"
}

func kubernetesAptChannel(version string) string {
	v := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(version), "v"))
	parts := strings.Split(v, ".")
	if len(parts) >= 2 {
		return "v" + parts[0] + "." + parts[1]
	}
	if v == "" {
		return "v1.31"
	}
	return "v" + v
}

const inventoryTemplate = `[control_plane]
{{- range .ControlPlanes }}
{{ .Name }} ansible_host={{ .IP }}
{{- end }}

[workers]
{{- range .Workers }}
{{ .Name }} ansible_host={{ .IP }}
{{- end }}

[k8s_cluster:children]
control_plane
workers

[all:vars]
ansible_user={{ .SSHUser }}
ansible_ssh_private_key_file={{ .SSHPrivateKeyPath }}
ansible_python_interpreter=/usr/bin/python3
`

const playbookTemplate = `---
- hosts: k8s_cluster
  become: true
  gather_facts: true
  tasks:
    - name: Disable swap
      command: swapoff -a
      changed_when: false

    - name: Remove swap from fstab
      replace:
        path: /etc/fstab
        regexp: '^([^#].*\\sswap\\s+sw\\s+.*)$'
        replace: '# \\1'

    - name: Load required kernel modules
      copy:
        dest: /etc/modules-load.d/k8s.conf
        content: |
          overlay
          br_netfilter

    - name: Ensure modules loaded
      shell: |
        modprobe overlay
        modprobe br_netfilter

    - name: Set sysctl params
      copy:
        dest: /etc/sysctl.d/k8s.conf
        content: |
          net.bridge.bridge-nf-call-iptables = 1
          net.bridge.bridge-nf-call-ip6tables = 1
          net.ipv4.ip_forward = 1

    - name: Apply sysctl
      command: sysctl --system
      changed_when: false

    - name: Install containerd and dependencies
      apt:
        name:
          - apt-transport-https
          - ca-certificates
          - curl
          - gpg
          - containerd
        update_cache: true
        state: present

    - name: Configure containerd with systemd cgroup
      shell: |
        mkdir -p /etc/containerd
        containerd config default > /etc/containerd/config.toml
        sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml
        systemctl restart containerd
        systemctl enable containerd

    - name: Add Kubernetes apt key
      shell: |
        mkdir -p /etc/apt/keyrings
        curl -fsSL https://pkgs.k8s.io/core:/stable:/{{ .KubernetesAptChannel }}/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
      args:
        creates: /etc/apt/keyrings/kubernetes-apt-keyring.gpg

    - name: Add Kubernetes apt repo
      copy:
        dest: /etc/apt/sources.list.d/kubernetes.list
        content: |
          deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/{{ .KubernetesAptChannel }}/deb/ /

    - name: Install kubeadm kubelet kubectl
      apt:
        name:
          - kubelet
          - kubeadm
          - kubectl
        update_cache: true
        state: present

    - name: Hold Kubernetes packages
      shell: apt-mark hold kubelet kubeadm kubectl
      changed_when: false

- hosts: control_plane[0]
  become: true
  tasks:
    - name: Initialize first control plane
      command: >
        kubeadm init
        --pod-network-cidr={{ .PodCIDR }}
        --service-cidr={{ .ServiceCIDR }}
        {{ .InitEndpointArg }}
      args:
        creates: /etc/kubernetes/admin.conf

    - name: Create join command
      command: kubeadm token create --print-join-command
      register: join_command

    - name: Save join command
      copy:
        content: "{{ "{{ join_command.stdout }}" }} --cri-socket unix:///run/containerd/containerd.sock"
        dest: /tmp/kubeadm_join_cmd.sh
        mode: '0700'

    - name: Install CNI
      shell: |
        export KUBECONFIG=/etc/kubernetes/admin.conf
        if [ "{{ .CNI }}" = "calico" ]; then
          kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.1/manifests/calico.yaml
        else
          kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.1/manifests/calico.yaml
        fi

- hosts: control_plane:workers:!control_plane[0]
  become: true
  tasks:
    - name: Read join command from first control plane
      slurp:
        src: /tmp/kubeadm_join_cmd.sh
      delegate_to: "{{ "{{ groups['control_plane'][0] }}" }}"
      run_once: true
      register: join_script

    - name: Join cluster
      shell: "{{ "{{ join_script.content | b64decode }}" }}"
      args:
        creates: /etc/kubernetes/kubelet.conf
`
