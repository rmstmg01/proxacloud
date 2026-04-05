package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type VMNode struct {
	VMName          string `json:"vm_name"`
	Role            string `json:"role"`
	RoleIndex       int    `json:"role_index"`
	ProxmoxNodeName string `json:"proxmox_node_name"`
	CPU             int    `json:"cpu"`
	MemoryMB        int    `json:"memory_mb"`
	DiskGB          int    `json:"disk_gb"`
	IPAddress       string `json:"ip_address,omitempty"`
}

type WorkspaceSpec struct {
	WorkspaceKey      string   `json:"-"`
	ClusterName       string   `json:"cluster_name"`
	KubernetesVersion string   `json:"kubernetes_version"`
	NetworkCIDR       string   `json:"network_cidr"`
	Gateway           string   `json:"gateway"`
	DNS               []string `json:"dns"`
	Bridge            string   `json:"bridge"`
	VLAN              int      `json:"vlan"`
	UbuntuImage       string   `json:"ubuntu_image"`
	SSHPublicKey      string   `json:"ssh_public_key"`
	Nodes             []VMNode `json:"nodes"`
}

type RunResult struct {
	WorkDir string `json:"work_dir"`
	Output  string `json:"output"`
	Status  string `json:"status"`
}

// ProxmoxAuth holds Proxmox credentials passed to Terraform as environment
// variables so the bpg/proxmox provider can authenticate.
type ProxmoxAuth struct {
	Endpoint       string
	Username       string
	Password       string
	APITokenID     string
	APITokenSecret string
	InsecureTLS    bool
	// SSHUsername / SSHPassword are used by the bpg/proxmox provider to SSH
	// into the Proxmox host for file uploads (e.g. cloud-init snippets).
	SSHUsername string
	SSHPassword string
}

// toEnv converts the auth struct to PROXMOX_VE_* environment variable strings.
func (a ProxmoxAuth) toEnv() []string {
	env := []string{
		"PROXMOX_VE_ENDPOINT=" + a.Endpoint,
		fmt.Sprintf("PROXMOX_VE_INSECURE=%t", a.InsecureTLS),
	}
	if a.APITokenID != "" && a.APITokenSecret != "" {
		env = append(env, "PROXMOX_VE_API_TOKEN="+a.APITokenID+"="+a.APITokenSecret)
	} else if a.Username != "" {
		env = append(env, "PROXMOX_VE_USERNAME="+a.Username)
		env = append(env, "PROXMOX_VE_PASSWORD="+a.Password)
	}
	if a.SSHUsername != "" {
		env = append(env, "PROXMOX_VE_SSH_USERNAME="+a.SSHUsername)
	}
	if a.SSHPassword != "" {
		env = append(env, "PROXMOX_VE_SSH_PASSWORD="+a.SSHPassword)
	}
	return env
}

type Runner struct {
	binary  string
	baseDir string
}

func NewRunner(binary, baseDir string) *Runner {
	if strings.TrimSpace(binary) == "" {
		binary = "terraform"
	}
	if strings.TrimSpace(baseDir) == "" {
		baseDir = filepath.Join("infra", "terraform", "runs")
	}
	return &Runner{binary: binary, baseDir: baseDir}
}

func (r *Runner) WorkspaceDir(clusterName string) string {
	return filepath.Join(r.baseDir, sanitizeName(clusterName))
}

func (r *Runner) WorkspaceDirForKey(workspaceKey string) string {
	return filepath.Join(r.baseDir, sanitizeName(workspaceKey))
}

func (r *Runner) PrepareWorkspace(spec WorkspaceSpec) (string, error) {
	if strings.TrimSpace(spec.ClusterName) == "" {
		return "", errors.New("cluster name is required")
	}
	if strings.TrimSpace(spec.UbuntuImage) == "" {
		return "", errors.New("ubuntu image path is required")
	}
	if strings.TrimSpace(spec.SSHPublicKey) == "" {
		return "", errors.New("ssh public key is required")
	}
	if len(spec.Nodes) == 0 {
		return "", errors.New("at least one node must be defined")
	}

	workspaceKey := strings.TrimSpace(spec.WorkspaceKey)
	if workspaceKey == "" {
		workspaceKey = spec.ClusterName
	}
	workDir := r.WorkspaceDirForKey(workspaceKey)
	if err := os.MkdirAll(filepath.Join(workDir, "cloud-init"), 0o755); err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}

	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(terraformMainTF), 0o644); err != nil {
		return "", fmt.Errorf("write main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "variables.tf"), []byte(terraformVariablesTF), 0o644); err != nil {
		return "", fmt.Errorf("write variables.tf: %w", err)
	}

	for _, n := range spec.Nodes {
		cloudInitPath := filepath.Join(workDir, "cloud-init", fmt.Sprintf("%s.yaml", n.VMName))
		content, err := renderCloudInit(spec, n)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(cloudInitPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write cloud-init file: %w", err)
		}
	}

	tfvars, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal terraform variables: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "terraform.tfvars.json"), tfvars, 0o644); err != nil {
		return "", fmt.Errorf("write terraform.tfvars.json: %w", err)
	}

	return workDir, nil
}

// buildEnv merges the current process environment with auth vars, stripping
// any pre-existing PROXMOX_VE_* variables so DB credentials always win.
func buildEnv(authEnv []string) []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PROXMOX_VE_") {
			filtered = append(filtered, e)
		}
	}
	return append(filtered, authEnv...)
}

func (r *Runner) Apply(workDir string, auth ProxmoxAuth) (RunResult, error) {
	output := strings.Builder{}
	authEnv := buildEnv(auth.toEnv())
	steps := [][]string{
		{"init", "-input=false"},
		{"validate"},
		{"plan", "-input=false", "-out=tfplan"},
		{"apply", "-input=false", "-auto-approve", "tfplan"},
	}

	for _, args := range steps {
		cmdOutput, err := r.execTerraform(workDir, authEnv, args...)
		output.WriteString(cmdOutput)
		output.WriteString("\n")
		if err != nil {
			fullOutput := strings.TrimSpace(output.String())
			if fullOutput == "" {
				return RunResult{WorkDir: workDir, Output: output.String(), Status: "failed"}, err
			}
			return RunResult{WorkDir: workDir, Output: output.String(), Status: "failed"}, fmt.Errorf("%w\nterraform output:\n%s", err, fullOutput)
		}
	}

	return RunResult{WorkDir: workDir, Output: output.String(), Status: "applied"}, nil
}

func (r *Runner) Destroy(workDir string, auth ProxmoxAuth) (RunResult, error) {
	output := strings.Builder{}
	authEnv := buildEnv(auth.toEnv())
	steps := [][]string{
		{"init", "-input=false"},
		{"destroy", "-input=false", "-auto-approve"},
	}

	for _, args := range steps {
		cmdOutput, err := r.execTerraform(workDir, authEnv, args...)
		output.WriteString(cmdOutput)
		output.WriteString("\n")
		if err != nil {
			fullOutput := strings.TrimSpace(output.String())
			if fullOutput == "" {
				return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroy-failed"}, err
			}
			return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroy-failed"}, fmt.Errorf("%w\nterraform output:\n%s", err, fullOutput)
		}
	}

	return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroyed"}, nil
}

func (r *Runner) execTerraform(workDir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = workDir
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := stdout.String() + stderr.String()
	if err != nil {
		return combined, fmt.Errorf("terraform %s failed: %w", strings.Join(args, " "), err)
	}

	return combined, nil
}

func renderCloudInit(spec WorkspaceSpec, node VMNode) (string, error) {
	tpl, err := template.New("cloudinit").Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse cloud-init template: %w", err)
	}

	payload := map[string]any{
		"VMName":       node.VMName,
		"Role":         node.Role,
		"ClusterName":  spec.ClusterName,
		"SSHPublicKey": spec.SSHPublicKey,
		"IPAddress":    node.IPAddress,
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, payload); err != nil {
		return "", fmt.Errorf("render cloud-init template: %w", err)
	}

	return out.String(), nil
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(name)))
}

const terraformMainTF = `terraform {
  required_version = ">= 1.6.0"

  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.66.0"
    }
  }
}

# Auth is supplied via PROXMOX_VE_* environment variables:
#   PROXMOX_VE_ENDPOINT, PROXMOX_VE_INSECURE
#   PROXMOX_VE_API_TOKEN   or   PROXMOX_VE_USERNAME + PROXMOX_VE_PASSWORD
#   PROXMOX_VE_SSH_USERNAME, PROXMOX_VE_SSH_PASSWORD
provider "proxmox" {
  ssh {
    agent = false
  }
}

variable "cluster_name" {}
variable "kubernetes_version" {}
variable "network_cidr" {}
variable "gateway" {}
variable "dns" { type = list(string) }
variable "bridge" {}
variable "vlan" { type = number }
variable "ubuntu_image" {}
variable "ssh_public_key" {}
variable "nodes" {
  type = list(object({
    vm_name           = string
    role              = string
    role_index        = number
    proxmox_node_name = string
    cpu               = number
    memory_mb         = number
    disk_gb           = number
    ip_address        = optional(string)
  }))
}

locals {
  node_map = {
    for n in var.nodes : n.vm_name => n
  }
	network_prefix = tonumber(split("/", var.network_cidr)[1])
}

# Upload cloud-init snippets to each Proxmox node via SSH before the VMs start.
resource "proxmox_virtual_environment_file" "cloud_init" {
  for_each = local.node_map

  content_type = "snippets"
  datastore_id = "local"
  node_name    = each.value.proxmox_node_name

  source_raw {
    data      = file("${path.module}/cloud-init/${each.value.vm_name}.yaml")
    file_name = "${each.value.vm_name}.yaml"
  }
}

resource "proxmox_virtual_environment_vm" "k8s" {
  for_each = local.node_map

  name      = each.value.vm_name
  node_name = each.value.proxmox_node_name

  depends_on = [proxmox_virtual_environment_file.cloud_init]

  initialization {
    user_data_file_id = proxmox_virtual_environment_file.cloud_init[each.key].id

		ip_config {
			ipv4 {
				address = each.value.ip_address != null && trimspace(each.value.ip_address) != "" ? "${each.value.ip_address}/${local.network_prefix}" : "dhcp"
				gateway = each.value.ip_address != null && trimspace(each.value.ip_address) != "" ? var.gateway : null
			}
		}
  }

  cpu {
    cores = each.value.cpu
  }

  memory {
    dedicated = each.value.memory_mb
  }

  network_device {
    bridge  = var.bridge
    vlan_id = var.vlan
  }

  disk {
    datastore_id = "local-lvm"
    file_id      = var.ubuntu_image
    interface    = "scsi0"
    size         = each.value.disk_gb
  }
}
`

const terraformVariablesTF = `# This file is intentionally minimal.
# Runtime values are written to terraform.tfvars.json by the backend provisioning engine.
`

const cloudInitTemplate = `#cloud-config
hostname: {{ .VMName }}
manage_etc_hosts: true
users:
  - name: ubuntu
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: sudo
    ssh_authorized_keys:
      - {{ .SSHPublicKey }}
package_update: true
packages:
  - qemu-guest-agent
  - curl
  - apt-transport-https
  - ca-certificates

runcmd:
  - [ systemctl, enable, --now, qemu-guest-agent ]
  - [ bash, -lc, "echo CLUSTER_NAME={{ .ClusterName }} > /etc/k8s-cluster.env" ]
  - [ bash, -lc, "echo NODE_ROLE={{ .Role }} >> /etc/k8s-cluster.env" ]
  - [ bash, -lc, "echo NODE_IP={{ .IPAddress }} >> /etc/k8s-cluster.env" ]
`
