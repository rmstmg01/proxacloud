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
	"sync"
	"text/template"
	"time"
)

const (
	ansibleProgressInterval = 5 * time.Second
	maxProgressOutputLength = 8000
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
	DeployKubeVIP     bool
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

	playbook, err := renderPlaybook(spec, workDir)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "bootstrap-k8s.yml"), []byte(playbook), 0o644); err != nil {
		return "", fmt.Errorf("write playbook: %w", err)
	}

	return workDir, nil
}

func (r *Runner) Apply(workDir string) (RunResult, error) {
	return r.ApplyWithProgress(workDir, nil)
}

func (r *Runner) ApplyWithProgress(workDir string, onProgress func(string)) (RunResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binary, "-i", "inventory.ini", "bootstrap-k8s.yml")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "ANSIBLE_HOST_KEY_CHECKING=False")

	var output synchronizedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return RunResult{WorkDir: workDir, Status: "failed"}, fmt.Errorf("start ansible playbook: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	lastOutputLength := 0
	emitProgress := func(allowEmpty bool) {
		if onProgress == nil {
			return
		}
		snapshot := output.String()
		if len(snapshot) == lastOutputLength && !allowEmpty {
			return
		}
		chunk := snapshot[lastOutputLength:]
		lastOutputLength = len(snapshot)
		onProgress(compactProgressOutput(chunk))
	}

	ticker := time.NewTicker(ansibleProgressInterval)
	defer ticker.Stop()

	var err error
	for {
		select {
		case err = <-done:
			emitProgress(false)
			combined := output.String()
			if err != nil {
				trimmed := strings.TrimSpace(combined)
				if trimmed == "" {
					return RunResult{WorkDir: workDir, Output: combined, Status: "failed"}, fmt.Errorf("run ansible playbook: %w", err)
				}
				return RunResult{WorkDir: workDir, Output: combined, Status: "failed"}, fmt.Errorf("run ansible playbook: %w\nansible output:\n%s", err, trimmed)
			}

			return RunResult{WorkDir: workDir, Output: combined, Status: "bootstrapped"}, nil
		case <-ticker.C:
			emitProgress(true)
		}
	}
}

type synchronizedBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buf.String()
}

func compactProgressOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= maxProgressOutputLength {
		return output
	}
	return "[output truncated; showing latest updates]\n" + output[len(output)-maxProgressOutputLength:]
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

func renderPlaybook(spec BootstrapSpec, workDir string) (string, error) {
	tpl, err := template.New("playbook").Parse(playbookTemplate)
	if err != nil {
		return "", fmt.Errorf("parse playbook template: %w", err)
	}

	controlPlaneCount := 0
	for _, n := range spec.Nodes {
		if n.Role == "control-plane" {
			controlPlaneCount++
		}
	}

	cni := strings.TrimSpace(spec.CNI)
	if cni == "" {
		cni = "calico"
	}

	payload := map[string]any{
		"KubernetesVersion":    strings.TrimSpace(spec.KubernetesVersion),
		"KubernetesAptChannel": kubernetesAptChannel(spec.KubernetesVersion),
		"ControlPlaneVIP":      strings.TrimSpace(spec.ControlPlaneVIP),
		"DeployKubeVIP":        spec.DeployKubeVIP,
		"PodCIDR":              nonEmpty(spec.PodCIDR, "10.244.0.0/16"),
		"ServiceCIDR":          nonEmpty(spec.ServiceCIDR, "10.96.0.0/12"),
		"CNI":                  cni,
		"InitEndpointArg":      initEndpointArg(spec.ControlPlaneVIP, primaryControlPlaneIP(spec.Nodes), controlPlaneCount, spec.DeployKubeVIP),
		"PlaybookDir":          workDir,
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

func initEndpointArg(vip, primaryControlPlaneIP string, controlPlaneCount int, deployKubeVIP bool) string {
	if controlPlaneCount <= 1 {
		return ""
	}

	endpoint := strings.TrimSpace(primaryControlPlaneIP)
	if deployKubeVIP {
		trimmedVIP := strings.TrimSpace(vip)
		if trimmedVIP != "" {
			endpoint = trimmedVIP
		}
	}
	if endpoint == "" {
		return ""
	}

	return " --control-plane-endpoint=" + endpoint + ":6443"
}

func primaryControlPlaneIP(nodes []Node) string {
	for _, n := range nodes {
		if n.Role == "control-plane" {
			return strings.TrimSpace(n.IP)
		}
	}
	return ""
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
ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o GlobalKnownHostsFile=/dev/null -o ConnectTimeout=10'
`

const playbookTemplate = `---
- hosts: k8s_cluster
  become: true
  gather_facts: false
  pre_tasks:
    - name: Wait for SSH connectivity (retry up to 5 times)
      wait_for_connection:
        connect_timeout: 5
        sleep: 2
        timeout: 20
      register: ssh_ready
      retries: 5
      delay: 10
      until: ssh_ready is succeeded

    - name: Gather facts after SSH is reachable
      setup:
  tasks:
    - name: Wait for cloud-init to finish
      shell: |
        if command -v cloud-init >/dev/null 2>&1; then
          cloud-init status --wait
        fi
      changed_when: false

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

    - name: Install containerd, Kubernetes, and storage dependencies
      apt:
        name:
          - apt-transport-https
          - ca-certificates
          - curl
          - gpg
          - conntrack
          - containerd
          - open-iscsi
          - nfs-common
        lock_timeout: 600
        update_cache: true
        state: present

    - name: Enable iSCSI daemon for Longhorn volumes
      systemd:
        name: iscsid
        enabled: true
        state: started

    - name: Configure containerd with systemd cgroup
      shell: |
        mkdir -p /etc/containerd
        containerd config default > /etc/containerd/config.toml
        sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml
        sed -i 's|sandbox_image = "registry.k8s.io/pause:.*"|sandbox_image = "registry.k8s.io/pause:3.10"|g' /etc/containerd/config.toml
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
        lock_timeout: 600
        update_cache: true
        state: present

    - name: Hold Kubernetes packages
      shell: apt-mark hold kubelet kubeadm kubectl
      changed_when: false

- hosts: control_plane[0]
  become: true
  tasks:
    - name: Validate control plane memory
      fail:
        msg: "Control-plane node requires at least 1700 MB RAM for kubeadm init (found {{ "{{ ansible_memtotal_mb }}" }} MB). Increase VM memory and retry."
      when: ansible_memtotal_mb | int < 1700

{{- if .DeployKubeVIP }}
    - name: Ensure kube-vip static manifest directory exists
      file:
        path: /etc/kubernetes/manifests
        state: directory
        mode: '0755'

    - name: Detect default network interface for kube-vip
      shell: ip -o -4 route show to default | awk '{print $5}' | head -n1
      register: kubevip_interface
      changed_when: false

    - name: Validate detected kube-vip interface
      fail:
        msg: "Unable to detect default network interface for kube-vip"
      when: kubevip_interface.stdout | trim == ""

    - name: Generate kube-vip static pod manifest
      shell: |
        ctr --namespace k8s.io image pull ghcr.io/kube-vip/kube-vip:v0.8.2
        ctr --namespace k8s.io run --rm --net-host ghcr.io/kube-vip/kube-vip:v0.8.2 vip /kube-vip manifest pod \
          --interface {{ "{{ kubevip_interface.stdout | trim }}" }} \
          --address {{ .ControlPlaneVIP }} \
          --controlplane \
          --services \
          --arp \
          --leaderElection \
          --k8sConfigPath /etc/kubernetes/admin.conf > /etc/kubernetes/manifests/kube-vip.yaml
      args:
        executable: /bin/bash

    - name: Use super-admin kubeconfig for kube-vip during kubeadm initialization
      replace:
        path: /etc/kubernetes/manifests/kube-vip.yaml
        regexp: 'path: /etc/kubernetes/admin\.conf'
        replace: 'path: /etc/kubernetes/super-admin.conf'
{{- end }}

    - name: Initialize first control plane
      command: >
        kubeadm init
        --cri-socket=unix:///run/containerd/containerd.sock
        --pod-network-cidr={{ .PodCIDR }}
        --service-cidr={{ .ServiceCIDR }}
        {{ .InitEndpointArg }}
      args:
        creates: /etc/kubernetes/admin.conf

{{- if .DeployKubeVIP }}
    - name: Use admin kubeconfig for kube-vip after kubeadm initialization
      replace:
        path: /etc/kubernetes/manifests/kube-vip.yaml
        regexp: 'path: /etc/kubernetes/super-admin\.conf'
        replace: 'path: /etc/kubernetes/admin.conf'
{{- end }}

    - name: Create join command
      command: kubeadm token create --print-join-command
      register: join_command

    - name: Upload control-plane certificates
      command: kubeadm init phase upload-certs --upload-certs
      register: upload_certs_output

    - name: Extract certificate key
      set_fact:
          kubeadm_certificate_key: "{{ "{{ upload_certs_output.stdout_lines[-1] | trim }}" }}"

    - name: Save worker join command
      copy:
          content: "{{ "{{ join_command.stdout }}" }} --cri-socket unix:///run/containerd/containerd.sock"
          dest: /tmp/kubeadm_join_worker.sh
          mode: '0700'

    - name: Save control-plane join command
      copy:
          content: "{{ "{{ join_command.stdout }}" }} --control-plane --certificate-key {{ "{{ kubeadm_certificate_key }}" }} --cri-socket unix:///run/containerd/containerd.sock"
          dest: /tmp/kubeadm_join_control_plane.sh
          mode: '0700'

    - name: Install CNI
      shell: |
        export KUBECONFIG=/etc/kubernetes/admin.conf
        if [ "{{ .CNI }}" = "calico" ]; then
          kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.1/manifests/calico.yaml
        else
          kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.1/manifests/calico.yaml
        fi

    - name: Create .kube directory for ansible user
      file:
        path: "/home/{{ "{{ ansible_user }}" }}/.kube"
        state: directory
        mode: '0755'
        owner: "{{ "{{ ansible_user }}" }}"
        group: "{{ "{{ ansible_user }}" }}"

    - name: Copy admin.conf to user kubeconfig
      copy:
        src: /etc/kubernetes/admin.conf
        dest: "/home/{{ "{{ ansible_user }}" }}/.kube/config"
        remote_src: true
        owner: "{{ "{{ ansible_user }}" }}"
        group: "{{ "{{ ansible_user }}" }}"
        mode: '0600'

- hosts: control_plane:!control_plane[0]
  become: true
  tasks:
    - name: Read control-plane join command from first control plane
      slurp:
        src: /tmp/kubeadm_join_control_plane.sh
      delegate_to: "{{ "{{ groups['control_plane'][0] }}" }}"
      run_once: true
      register: cp_join_script

    - name: Join secondary control plane
      shell: "{{ "{{ cp_join_script.content | b64decode }}" }}"
      args:
        creates: /etc/kubernetes/kubelet.conf

{{- if .DeployKubeVIP }}
    - name: Detect default network interface for kube-vip
      shell: ip -o -4 route show to default | awk '{print $5}' | head -n1
      register: kubevip_interface
      changed_when: false

    - name: Validate detected kube-vip interface
      fail:
        msg: "Unable to detect default network interface for kube-vip"
      when: kubevip_interface.stdout | trim == ""

    - name: Generate kube-vip static pod manifest on secondary control plane
      shell: |
        ctr --namespace k8s.io image pull ghcr.io/kube-vip/kube-vip:v0.8.2
        ctr --namespace k8s.io run --rm --net-host ghcr.io/kube-vip/kube-vip:v0.8.2 vip /kube-vip manifest pod \
          --interface {{ "{{ kubevip_interface.stdout | trim }}" }} \
          --address {{ .ControlPlaneVIP }} \
          --controlplane \
          --services \
          --arp \
          --leaderElection \
          --k8sConfigPath /etc/kubernetes/admin.conf > /etc/kubernetes/manifests/kube-vip.yaml
      args:
        executable: /bin/bash
{{- end }}

- hosts: workers
  become: true
  tasks:
    - name: Read worker join command from first control plane
      slurp:
        src: /tmp/kubeadm_join_worker.sh
      delegate_to: "{{ "{{ groups['control_plane'][0] }}" }}"
      run_once: true
      register: worker_join_script

    - name: Join worker node
      shell: "{{ "{{ worker_join_script.content | b64decode }}" }}"
      args:
        creates: /etc/kubernetes/kubelet.conf

- hosts: control_plane[0]
  become: true
  tasks:
    - name: Label worker nodes
      shell: >
        kubectl --kubeconfig=/etc/kubernetes/admin.conf
        label node {{ "{{ item }}" }}
        node-role.kubernetes.io/worker=worker
        --overwrite
      loop: "{{ "{{ groups['workers'] }}" }}"
      when: groups['workers'] | length > 0

- hosts: control_plane[0]
  become: true
  tasks:
    - name: Fetch kubeconfig admin.conf
      fetch:
        src: /etc/kubernetes/admin.conf
        dest: "{{ .PlaybookDir }}/admin.conf"
        flat: yes
      run_once: true
`
