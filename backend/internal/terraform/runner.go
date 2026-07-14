package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	terraformProgressInterval        = 5 * time.Second
	maxTerraformProgressOutputLength = 8000
)

const defaultProxmoxSSHPort = 9846

type VMNode struct {
	VMName              string `json:"vm_name"`
	Role                string `json:"role"`
	RoleIndex           int    `json:"role_index"`
	ProxmoxNodeName     string `json:"proxmox_node_name"`
	ProxmoxNodeAddress  string `json:"proxmox_node_address,omitempty"`
	CPU                 int    `json:"cpu"`
	MemoryMB            int    `json:"memory_mb"`
	DiskGB              int    `json:"disk_gb"`
	IPAddress           string `json:"ip_address,omitempty"`
	NetworkCIDR         string `json:"network_cidr"`
	Gateway             string `json:"gateway"`
	Bridge              string `json:"bridge"`
	VLAN                int    `json:"vlan"`
	AgentInstallCommand string `json:"-"`
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
	CustomerPublicKey string   `json:"customer_public_key,omitempty"`
	Nodes             []VMNode `json:"nodes"`
}

type StandaloneVPSSpec struct {
	WorkspaceKey        string   `json:"-"`
	Name                string   `json:"name"`
	ProxmoxNodeName     string   `json:"proxmox_node_name"`
	CPU                 int      `json:"cpu"`
	MemoryMB            int      `json:"memory_mb"`
	DiskGB              int      `json:"disk_gb"`
	NetworkCIDR         string   `json:"network_cidr"`
	Gateway             string   `json:"gateway"`
	DNS                 []string `json:"dns"`
	Bridge              string   `json:"bridge"`
	VLAN                int      `json:"vlan"`
	CloudInitImage      string   `json:"cloud_init_image"`
	OSType              string   `json:"os_type"`
	SSHPublicKey        string   `json:"ssh_public_key"`
	CustomerPublicKey   string   `json:"customer_public_key,omitempty"`
	SSHUser             string   `json:"ssh_user"`
	RootPassword        string   `json:"-"`
	IPAddress           string   `json:"ip_address,omitempty"`
	AgentInstallCommand string   `json:"-"`
}

type StandaloneContainerSpec struct {
	WorkspaceKey    string   `json:"-"`
	Name            string   `json:"name"`
	ProxmoxNodeName string   `json:"proxmox_node_name"`
	CPU             int      `json:"cpu"`
	MemoryMB        int      `json:"memory_mb"`
	DiskGB          int      `json:"disk_gb"`
	NetworkCIDR     string   `json:"network_cidr"`
	Gateway         string   `json:"gateway"`
	DNS             []string `json:"dns"`
	Bridge          string   `json:"bridge"`
	VLAN            int      `json:"vlan"`
	TemplateImage   string   `json:"template_image"`
	OSType          string   `json:"os_type"`
	SSHPublicKey    string   `json:"ssh_public_key"`
	RootPassword    string   `json:"root_password"`
	IPAddress       string   `json:"ip_address,omitempty"`
	Unprivileged    bool     `json:"unprivileged"`
	Nesting         bool     `json:"nesting"`
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
	binary    string
	baseDir   string
	pluginDir string
}

func NewRunner(binary, baseDir, pluginDir string) *Runner {
	if strings.TrimSpace(binary) == "" {
		binary = "terraform"
	}
	if strings.TrimSpace(baseDir) == "" {
		baseDir = filepath.Join("infra", "terraform", "runs")
	}
	return &Runner{binary: binary, baseDir: baseDir, pluginDir: strings.TrimSpace(pluginDir)}
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

	if err := r.writeStaticWorkspaceFilesForNodes(workDir, spec.Nodes, ""); err != nil {
		return "", err
	}

	for _, n := range spec.Nodes {
		cloudInitPath := filepath.Join(workDir, "cloud-init", fmt.Sprintf("%s.yaml", n.VMName))
		content, err := renderCloudInit(spec, n)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(cloudInitPath, []byte(content), 0o600); err != nil {
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

func (r *Runner) PrepareStandaloneVPSWorkspace(spec StandaloneVPSSpec) (string, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return "", errors.New("vps name is required")
	}
	if strings.TrimSpace(spec.ProxmoxNodeName) == "" {
		return "", errors.New("proxmox node name is required")
	}
	if strings.TrimSpace(spec.CloudInitImage) == "" {
		return "", errors.New("cloud-init image path is required")
	}
	if strings.TrimSpace(spec.SSHPublicKey) == "" && strings.TrimSpace(spec.RootPassword) == "" {
		return "", errors.New("provide an ssh public key or root password")
	}
	if spec.CPU <= 0 {
		spec.CPU = 1
	}
	if spec.MemoryMB <= 0 {
		spec.MemoryMB = 1024
	}
	if spec.DiskGB <= 0 {
		spec.DiskGB = 20
	}
	spec.OSType = strings.ToLower(strings.TrimSpace(spec.OSType))
	if spec.OSType == "" {
		spec.OSType = "generic"
	}
	spec.SSHUser = "root"

	workspaceKey := strings.TrimSpace(spec.WorkspaceKey)
	if workspaceKey == "" {
		workspaceKey = "vps-" + spec.Name
	}
	workDir := r.WorkspaceDirForKey(workspaceKey)
	if err := os.MkdirAll(filepath.Join(workDir, "cloud-init"), 0o755); err != nil {
		return "", fmt.Errorf("create vps workspace: %w", err)
	}

	if err := r.writeStandaloneVPSStaticFiles(workDir, ""); err != nil {
		return "", err
	}

	cloudInitContent, err := renderStandaloneVPSCloudInit(spec)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workDir, "cloud-init", "user-data.yaml"), []byte(cloudInitContent), 0o644); err != nil {
		return "", fmt.Errorf("write vps cloud-init file: %w", err)
	}

	tfvars, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal vps terraform variables: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "terraform.tfvars.json"), tfvars, 0o644); err != nil {
		return "", fmt.Errorf("write vps terraform.tfvars.json: %w", err)
	}

	return workDir, nil
}

func (r *Runner) PrepareStandaloneContainerWorkspace(spec StandaloneContainerSpec) (string, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return "", errors.New("container name is required")
	}
	if strings.TrimSpace(spec.ProxmoxNodeName) == "" {
		return "", errors.New("proxmox node name is required")
	}
	if strings.TrimSpace(spec.TemplateImage) == "" {
		return "", errors.New("container template path is required")
	}
	if strings.TrimSpace(spec.RootPassword) == "" {
		return "", errors.New("root password is required for LXC containers")
	}
	if spec.CPU <= 0 {
		spec.CPU = 1
	}
	if spec.MemoryMB <= 0 {
		spec.MemoryMB = 512
	}
	if spec.DiskGB <= 0 {
		spec.DiskGB = 8
	}
	if strings.TrimSpace(spec.OSType) == "" {
		spec.OSType = "debian"
	}

	workspaceKey := strings.TrimSpace(spec.WorkspaceKey)
	if workspaceKey == "" {
		workspaceKey = "container-" + spec.Name
	}
	workDir := r.WorkspaceDirForKey(workspaceKey)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("create container workspace: %w", err)
	}
	if err := r.writeStandaloneContainerStaticFiles(workDir, ""); err != nil {
		return "", err
	}

	tfvars, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal container terraform variables: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "terraform.tfvars.json"), tfvars, 0o644); err != nil {
		return "", fmt.Errorf("write container terraform.tfvars.json: %w", err)
	}
	return workDir, nil
}

// initArgs returns the terraform init command arguments, using a local plugin
// directory when one is configured to avoid calls to registry.terraform.io.
func (r *Runner) initArgs() []string {
	args := []string{"init", "-input=false"}
	if r.pluginDir != "" {
		args = append(args, "-plugin-dir="+r.pluginDir)
	}
	return args
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
	return r.ApplyWithProgress(workDir, auth, nil)
}

func (r *Runner) ApplyStandaloneVPS(workDir string, auth ProxmoxAuth) (RunResult, error) {
	if err := r.writeStandaloneVPSStaticFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "failed"}, err
	}
	return r.applyPreparedWorkspace(workDir, auth)
}

func (r *Runner) ApplyStandaloneContainer(workDir string, auth ProxmoxAuth) (RunResult, error) {
	if err := r.writeStandaloneContainerStaticFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "failed"}, err
	}
	return r.applyPreparedWorkspace(workDir, auth)
}

func (r *Runner) ApplyWithProgress(workDir string, auth ProxmoxAuth, onProgress func(string)) (RunResult, error) {
	if err := r.writeStaticWorkspaceFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "failed"}, err
	}
	return r.applyPreparedWorkspaceWithProgress(workDir, auth, onProgress)
}

func (r *Runner) applyPreparedWorkspace(workDir string, auth ProxmoxAuth) (RunResult, error) {
	return r.applyPreparedWorkspaceWithProgress(workDir, auth, nil)
}

func (r *Runner) applyPreparedWorkspaceWithProgress(workDir string, auth ProxmoxAuth, onProgress func(string)) (RunResult, error) {
	output := strings.Builder{}
	if snippetMessage, err := prepareCloudInitSnippetStorage(workDir, auth); err != nil {
		return RunResult{WorkDir: workDir, Output: output.String(), Status: "failed"}, err
	} else if snippetMessage != "" && onProgress != nil {
		onProgress(snippetMessage)
	}
	if aliasMessage, err := prepareCloudImageImportAlias(workDir, auth); err != nil {
		return RunResult{WorkDir: workDir, Output: output.String(), Status: "failed"}, err
	} else if aliasMessage != "" && onProgress != nil {
		onProgress(aliasMessage)
	}
	authEnv := buildEnv(auth.toEnv())
	initArgs := r.initArgs()
	steps := [][]string{
		initArgs,
		{"validate"},
		{"plan", "-input=false", "-out=tfplan"},
		{"apply", "-input=false", "-auto-approve", "tfplan"},
	}

	for _, args := range steps {
		if onProgress != nil {
			onProgress(fmt.Sprintf("$ terraform %s", strings.Join(args, " ")))
		}
		cmdOutput, err := r.execTerraformWithProgress(workDir, authEnv, onProgress, args...)
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

var safeProxmoxImportComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var proxacloudEnrollmentTokenPattern = regexp.MustCompile(`pct_[A-Za-z0-9_-]+`)

const prepareSnippetStorageCommand = `set -eu
storage_json=$(pvesh get /storage/local --output-format json)
base=$(printf '%s' "$storage_json" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
content=$(printf '%s' "$storage_json" | sed -n 's/.*"content"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
test -n "$base"
mkdir -p "$base/snippets"
case ",$content," in
  *,snippets,*) ;;
  *) pvesm set local --content "${content:+$content,}snippets" >/dev/null ;;
esac`

func prepareCloudInitSnippetStorage(workDir string, auth ProxmoxAuth) (string, error) {
	tfvarsPath := filepath.Join(workDir, "terraform.tfvars.json")
	raw, err := os.ReadFile(tfvarsPath)
	if err != nil {
		return "", fmt.Errorf("read terraform workspace variables: %w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return "", fmt.Errorf("read terraform workspace variables: %w", err)
	}

	defaultHost := proxmoxSSHAddress(auth.Endpoint)
	hosts := make([]string, 0, 1)
	seen := map[string]struct{}{}
	addHost := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		if _, exists := seen[host]; exists {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	if nodes, ok := values["nodes"].([]any); ok {
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]any)
			if !ok {
				continue
			}
			address, _ := node["proxmox_node_address"].(string)
			if strings.TrimSpace(address) == "" {
				address = defaultHost
			}
			addHost(address)
		}
	}
	if len(hosts) == 0 {
		addHost(defaultHost)
	}
	if len(hosts) == 0 {
		return "", errors.New("cannot determine a Proxmox SSH address for cloud-init snippet preparation")
	}

	for _, host := range hosts {
		client, err := dialProxmoxSSH(host, auth)
		if err != nil {
			return "", fmt.Errorf("connect to Proxmox host %s to prepare cloud-init snippets: %w", host, err)
		}
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			return "", fmt.Errorf("open Proxmox SSH session on %s to prepare cloud-init snippets: %w", host, err)
		}
		commandOutput, runErr := session.CombinedOutput(prepareSnippetStorageCommand)
		_ = session.Close()
		_ = client.Close()
		if runErr != nil {
			return "", fmt.Errorf("prepare cloud-init snippet storage on Proxmox host %s: %w: %s", host, runErr, strings.TrimSpace(string(commandOutput)))
		}
	}
	return "Prepared Proxmox local snippet storage for cloud-init", nil
}

// prepareCloudImageImportAlias handles Ubuntu-style cloud images whose file
// name ends in .img. Proxmox can list those files as import content, but its
// directory-storage parser only accepts raw, qcow2 and vmdk extensions when a
// VM consumes import-from. The alias is a hard link (or symlink fallback) on
// the shared storage, so the image remains on NFS and is not copied per node.
func prepareCloudImageImportAlias(workDir string, auth ProxmoxAuth) (string, error) {
	tfvarsPath := filepath.Join(workDir, "terraform.tfvars.json")
	raw, err := os.ReadFile(tfvarsPath)
	if err != nil {
		return "", fmt.Errorf("read terraform workspace variables: %w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return "", fmt.Errorf("read terraform workspace variables: %w", err)
	}

	imageKey := ""
	imageID := ""
	for _, key := range []string{"ubuntu_image", "cloud_init_image"} {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			imageKey = key
			imageID = strings.TrimSpace(value)
			break
		}
	}
	storageID, filename, ok := imgImportVolumeParts(imageID)
	if !ok {
		return "", nil
	}
	if !safeProxmoxImportComponent.MatchString(storageID) || !safeProxmoxImportComponent.MatchString(filename) {
		return "", fmt.Errorf("cloud image import path %q contains unsupported characters", imageID)
	}
	if strings.TrimSpace(auth.SSHUsername) == "" || strings.TrimSpace(auth.SSHPassword) == "" {
		return "", fmt.Errorf("cloud image %q needs a Proxmox-compatible import alias; Proxmox SSH username and password are required to create it on the shared NFS storage", imageID)
	}

	host := proxmoxSSHAddress(auth.Endpoint)
	if nodes, ok := values["nodes"].([]any); ok && len(nodes) > 0 {
		if first, ok := nodes[0].(map[string]any); ok {
			if address, ok := first["proxmox_node_address"].(string); ok && strings.TrimSpace(address) != "" {
				host = strings.TrimSpace(address)
			}
		}
	}
	if host == "" {
		return "", errors.New("cannot determine a Proxmox SSH address for cloud image import preparation")
	}

	sourcePath := "/mnt/pve/" + storageID + "/import/" + filename
	command := fmt.Sprintf(
		"set -eu; source=%s; test -r \"$source\"; format=$(qemu-img info \"$source\" | sed -n 's/^file format: //p' | head -n 1); case \"$format\" in qcow2|raw|vmdk) ;; *) echo \"unsupported image format: $format\" >&2; exit 1 ;; esac; target=\"${source%%.img}.$format\"; if [ ! -e \"$target\" ]; then ln \"$source\" \"$target\" 2>/dev/null || ln -s \"$source\" \"$target\"; fi; test -r \"$target\"; printf '%%s' \"$format\"",
		shellQuote(sourcePath),
	)
	client, err := dialProxmoxSSH(host, auth)
	if err != nil {
		return "", fmt.Errorf("connect to Proxmox over SSH to prepare shared cloud image: %w", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("open Proxmox SSH session to prepare shared cloud image: %w", err)
	}
	formatOutput, err := session.CombinedOutput(command)
	_ = session.Close()
	if err != nil {
		return "", fmt.Errorf("prepare shared cloud image %q: %w: %s", imageID, err, strings.TrimSpace(string(formatOutput)))
	}
	format := strings.TrimSpace(string(formatOutput))
	if format != "qcow2" && format != "raw" && format != "vmdk" {
		return "", fmt.Errorf("prepare shared cloud image %q returned unsupported format %q", imageID, format)
	}

	aliasFilename := strings.TrimSuffix(filename, filepath.Ext(filename)) + "." + format
	aliasID := storageID + ":import/" + aliasFilename
	values[imageKey] = aliasID
	updated, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return "", fmt.Errorf("update terraform workspace image alias: %w", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(tfvarsPath, updated, 0o644); err != nil {
		return "", fmt.Errorf("update terraform workspace image alias: %w", err)
	}
	return fmt.Sprintf("Prepared shared NFS cloud image alias %s for Proxmox import", aliasID), nil
}

func dialProxmoxSSH(host string, auth ProxmoxAuth) (*ssh.Client, error) {
	if strings.TrimSpace(auth.SSHUsername) == "" || strings.TrimSpace(auth.SSHPassword) == "" {
		return nil, errors.New("Proxmox SSH username and password are required")
	}
	clientConfig := &ssh.ClientConfig{
		User:            strings.TrimSpace(auth.SSHUsername),
		Auth:            []ssh.AuthMethod{ssh.Password(auth.SSHPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Matches the provider's current SSH behavior.
		Timeout:         20 * time.Second,
	}
	return ssh.Dial("tcp", net.JoinHostPort(strings.TrimSpace(host), fmt.Sprintf("%d", defaultProxmoxSSHPort)), clientConfig)
}

func imgImportVolumeParts(volumeID string) (string, string, bool) {
	trimmed := strings.TrimSpace(volumeID)
	separator := strings.Index(strings.ToLower(trimmed), ":import/")
	if separator <= 0 {
		return "", "", false
	}
	storageID := trimmed[:separator]
	filename := trimmed[separator+len(":import/"):]
	if !strings.EqualFold(filepath.Ext(filename), ".img") || strings.ContainsAny(filename, `/\\`) {
		return "", "", false
	}
	return storageID, filename, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (r *Runner) DestroyStandaloneVPS(workDir string, auth ProxmoxAuth) (RunResult, error) {
	if err := r.writeStandaloneVPSStaticFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "destroy-failed"}, err
	}
	return r.destroyPreparedWorkspace(workDir, auth)
}

func (r *Runner) DestroyStandaloneContainer(workDir string, auth ProxmoxAuth) (RunResult, error) {
	if err := r.writeStandaloneContainerStaticFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "destroy-failed"}, err
	}
	return r.destroyPreparedWorkspace(workDir, auth)
}

func (r *Runner) Destroy(workDir string, auth ProxmoxAuth) (RunResult, error) {
	if err := r.writeStaticWorkspaceFiles(workDir, auth.Endpoint); err != nil {
		return RunResult{WorkDir: workDir, Output: "", Status: "destroy-failed"}, err
	}
	return r.destroyPreparedWorkspace(workDir, auth)
}

func (r *Runner) destroyPreparedWorkspace(workDir string, auth ProxmoxAuth) (RunResult, error) {
	output := strings.Builder{}
	authEnv := buildEnv(auth.toEnv())
	initArgs := r.initArgs()
	steps := [][]string{
		initArgs,
		{"destroy", "-input=false", "-auto-approve", "-lock-timeout=2m"},
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
			if strings.Contains(fullOutput, "Error acquiring the state lock") {
				return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroy-failed"}, fmt.Errorf("%w\nterraform state is locked for workspace %s; if no Terraform operation is still running, unlock it manually with terraform force-unlock <LOCK_ID>\nterraform output:\n%s", err, workDir, fullOutput)
			}
			return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroy-failed"}, fmt.Errorf("%w\nterraform output:\n%s", err, fullOutput)
		}
	}

	return RunResult{WorkDir: workDir, Output: output.String(), Status: "destroyed"}, nil
}

func (r *Runner) execTerraform(workDir string, env []string, args ...string) (string, error) {
	return r.execTerraformWithProgress(workDir, env, nil, args...)
}

func (r *Runner) execTerraformWithProgress(workDir string, env []string, onProgress func(string), args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = workDir
	cmd.Env = env

	var output synchronizedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start terraform %s: %w", strings.Join(args, " "), err)
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
		onProgress(compactTerraformProgressOutput(redactTerraformSecrets(chunk)))
	}

	ticker := time.NewTicker(terraformProgressInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			emitProgress(false)
			combined := redactTerraformSecrets(output.String())
			if err != nil {
				return combined, fmt.Errorf("terraform %s failed: %w", strings.Join(args, " "), err)
			}
			return combined, nil
		case <-ticker.C:
			emitProgress(true)
		}
	}
}

func redactTerraformSecrets(output string) string {
	return proxacloudEnrollmentTokenPattern.ReplaceAllString(output, "pct_[REDACTED]")
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

func compactTerraformProgressOutput(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= maxTerraformProgressOutputLength {
		return output
	}
	return output[len(output)-maxTerraformProgressOutputLength:]
}

func (r *Runner) writeStaticWorkspaceFiles(workDir string, endpoint string) error {
	if strings.TrimSpace(workDir) == "" {
		return errors.New("terraform workspace directory is required")
	}

	nodes := []VMNode{}
	tfvarsPath := filepath.Join(workDir, "terraform.tfvars.json")
	if raw, err := os.ReadFile(tfvarsPath); err == nil {
		var spec WorkspaceSpec
		if unmarshalErr := json.Unmarshal(raw, &spec); unmarshalErr != nil {
			return fmt.Errorf("read terraform workspace variables: %w", unmarshalErr)
		}
		nodes = spec.Nodes
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read terraform workspace variables: %w", err)
	}

	return r.writeStaticWorkspaceFilesForNodes(workDir, nodes, endpoint)
}

func (r *Runner) writeStaticWorkspaceFilesForNodes(workDir string, nodes []VMNode, endpoint string) error {
	mainTF, err := renderTerraformMainTF(nodes, endpoint)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0o644); err != nil {
		return fmt.Errorf("write main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "variables.tf"), []byte(terraformVariablesTF), 0o644); err != nil {
		return fmt.Errorf("write variables.tf: %w", err)
	}
	return nil
}

func (r *Runner) writeStandaloneVPSStaticFiles(workDir string, endpoint string) error {
	if strings.TrimSpace(workDir) == "" {
		return errors.New("terraform workspace directory is required")
	}
	mainTF := strings.Replace(standaloneVPSTerraformMainTF, "{{ .SSHNodeBlocks }}", standaloneVPSSSHNodeBlocks(workDir, endpoint), 1)
	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0o644); err != nil {
		return fmt.Errorf("write vps main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "variables.tf"), []byte(terraformVariablesTF), 0o644); err != nil {
		return fmt.Errorf("write vps variables.tf: %w", err)
	}
	return nil
}

func (r *Runner) writeStandaloneContainerStaticFiles(workDir string, endpoint string) error {
	if strings.TrimSpace(workDir) == "" {
		return errors.New("terraform workspace directory is required")
	}
	mainTF := strings.Replace(standaloneContainerTerraformMainTF, "{{ .SSHNodeBlocks }}", standaloneContainerSSHNodeBlocks(workDir, endpoint), 1)
	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0o644); err != nil {
		return fmt.Errorf("write container main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "variables.tf"), []byte(terraformVariablesTF), 0o644); err != nil {
		return fmt.Errorf("write container variables.tf: %w", err)
	}
	return nil
}

func standaloneContainerSSHNodeBlocks(workDir string, endpoint string) string {
	nodeName := ""
	tfvarsPath := filepath.Join(workDir, "terraform.tfvars.json")
	if raw, err := os.ReadFile(tfvarsPath); err == nil {
		var spec StandaloneContainerSpec
		if json.Unmarshal(raw, &spec) == nil {
			nodeName = strings.TrimSpace(spec.ProxmoxNodeName)
		}
	}
	if nodeName == "" {
		return ""
	}
	address := proxmoxSSHAddress(endpoint)
	if address == "" {
		address = nodeName
	}
	return fmt.Sprintf(`
    node {
      name    = %q
      address = %q
      port    = %d
    }
`, nodeName, address, defaultProxmoxSSHPort)
}

func standaloneVPSSSHNodeBlocks(workDir string, endpoint string) string {
	nodeName := ""
	tfvarsPath := filepath.Join(workDir, "terraform.tfvars.json")
	if raw, err := os.ReadFile(tfvarsPath); err == nil {
		var spec StandaloneVPSSpec
		if json.Unmarshal(raw, &spec) == nil {
			nodeName = strings.TrimSpace(spec.ProxmoxNodeName)
		}
	}
	if nodeName == "" {
		return ""
	}
	address := proxmoxSSHAddress(endpoint)
	if address == "" {
		address = nodeName
	}
	return fmt.Sprintf(`
    node {
      name    = %q
      address = %q
      port    = %d
    }
`, nodeName, address, defaultProxmoxSSHPort)
}

func renderTerraformMainTF(nodes []VMNode, endpoint string) (string, error) {
	nodeNames := make([]string, 0)
	seen := make(map[string]struct{}, len(nodes))
	addressesByNode := make(map[string]string, len(nodes))
	for _, node := range nodes {
		name := strings.TrimSpace(node.ProxmoxNodeName)
		if name == "" {
			continue
		}
		if address := strings.TrimSpace(node.ProxmoxNodeAddress); address != "" {
			addressesByNode[name] = address
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)

	address := proxmoxSSHAddress(endpoint)
	var nodeBlocks strings.Builder
	for _, name := range nodeNames {
		nodeAddress := name
		if explicitAddress := strings.TrimSpace(addressesByNode[name]); explicitAddress != "" {
			nodeAddress = explicitAddress
		} else if address != "" {
			nodeAddress = address
		}
		nodeBlocks.WriteString("\n    node {\n")
		nodeBlocks.WriteString(fmt.Sprintf("      name    = %q\n", name))
		nodeBlocks.WriteString(fmt.Sprintf("      address = %q\n", nodeAddress))
		nodeBlocks.WriteString(fmt.Sprintf("      port    = %d\n", defaultProxmoxSSHPort))
		nodeBlocks.WriteString("    }\n")
	}

	return strings.Replace(terraformMainTF, "{{ .SSHNodeBlocks }}", nodeBlocks.String(), 1), nil
}

func proxmoxSSHAddress(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		host = strings.TrimSpace(parsed.Host)
	}
	if host == "" {
		return ""
	}
	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = withoutPort
	}
	return strings.Trim(host, "[]")
}

func renderCloudInit(spec WorkspaceSpec, node VMNode) (string, error) {
	tpl, err := template.New("cloudinit").Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse cloud-init template: %w", err)
	}

	payload := map[string]any{
		"VMName":              node.VMName,
		"Role":                node.Role,
		"ClusterName":         spec.ClusterName,
		"SSHPublicKey":        spec.SSHPublicKey,
		"CustomerPublicKey":   strings.TrimSpace(spec.CustomerPublicKey),
		"IPAddress":           node.IPAddress,
		"AgentInstallCommand": strings.TrimSpace(node.AgentInstallCommand),
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, payload); err != nil {
		return "", fmt.Errorf("render cloud-init template: %w", err)
	}

	return out.String(), nil
}

func renderStandaloneVPSCloudInit(spec StandaloneVPSSpec) (string, error) {
	tpl, err := template.New("vps-cloudinit").Parse(standaloneVPSCloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parse vps cloud-init template: %w", err)
	}

	payload := map[string]any{
		"Name":                strings.TrimSpace(spec.Name),
		"SSHUser":             strings.TrimSpace(spec.SSHUser),
		"SSHPublicKey":        strings.TrimSpace(spec.SSHPublicKey),
		"CustomerPublicKey":   strings.TrimSpace(spec.CustomerPublicKey),
		"RootPassword":        strings.TrimSpace(spec.RootPassword),
		"IPAddress":           strings.TrimSpace(spec.IPAddress),
		"AgentInstallCommand": strings.TrimSpace(spec.AgentInstallCommand),
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, payload); err != nil {
		return "", fmt.Errorf("render vps cloud-init template: %w", err)
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
{{ .SSHNodeBlocks }}
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
    proxmox_node_address = optional(string)
    cpu               = number
    memory_mb         = number
    disk_gb           = number
    ip_address        = optional(string)
		network_cidr      = string
		gateway           = string
		bridge            = string
		vlan              = number
  }))
}

locals {
	ubuntu_image_is_import = can(regex(":import/", var.ubuntu_image))
  node_map = {
    for n in var.nodes : n.vm_name => n
  }
}

# Upload cloud-init snippets to each Proxmox node via SSH before the VMs start.
resource "proxmox_virtual_environment_file" "cloud_init" {
  for_each = local.node_map

  content_type = "snippets"
  datastore_id = "local"
  node_name    = each.value.proxmox_node_name

  source_raw {
    data      = sensitive(file("${path.module}/cloud-init/${each.value.vm_name}.yaml"))
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
				address = each.value.ip_address != null && trimspace(each.value.ip_address) != "" ? "${each.value.ip_address}/${tonumber(split("/", each.value.network_cidr)[1])}" : "dhcp"
				gateway = each.value.ip_address != null && trimspace(each.value.ip_address) != "" ? each.value.gateway : null
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
	bridge  = each.value.bridge
	vlan_id = each.value.vlan
  }

  disk {
    datastore_id = "local-lvm"
    file_id      = local.ubuntu_image_is_import ? null : var.ubuntu_image
    import_from  = local.ubuntu_image_is_import ? var.ubuntu_image : null
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
{{- if .CustomerPublicKey }}
      - {{ .CustomerPublicKey }}
{{- end }}
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
{{- if .AgentInstallCommand }}
  - [ bash, -lc, {{ printf "%q" .AgentInstallCommand }} ]
{{- end }}
`

const standaloneVPSTerraformMainTF = `terraform {
  required_version = ">= 1.6.0"

  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.66.0"
    }
  }
}

provider "proxmox" {
  ssh {
    agent = false
{{ .SSHNodeBlocks }}
  }
}

variable "name" {}
variable "proxmox_node_name" {}
variable "cpu" { type = number }
variable "memory_mb" { type = number }
variable "disk_gb" { type = number }
variable "network_cidr" {}
variable "gateway" {}
variable "dns" { type = list(string) }
variable "bridge" {}
variable "vlan" { type = number }
variable "cloud_init_image" {}
variable "os_type" {
  type    = string
  default = "generic"
}
variable "ssh_public_key" {}
variable "ssh_user" {}
variable "ip_address" {
  type    = string
  default = ""
}

locals {
  cloud_init_image_is_import = can(regex(":import/", var.cloud_init_image))
  is_debian                  = lower(trimspace(var.os_type)) == "debian"
}

resource "proxmox_virtual_environment_file" "cloud_init" {
  content_type = "snippets"
  datastore_id = "local"
  node_name    = var.proxmox_node_name

  source_raw {
    data      = sensitive(file("${path.module}/cloud-init/user-data.yaml"))
    file_name = "${var.name}-user-data.yaml"
  }
}

resource "proxmox_virtual_environment_vm" "vps" {
  name      = var.name
  node_name = var.proxmox_node_name

  depends_on = [proxmox_virtual_environment_file.cloud_init]

  agent {
    enabled = true
  }

  initialization {
    interface         = local.is_debian ? "scsi1" : null
    user_data_file_id = proxmox_virtual_environment_file.cloud_init.id

    ip_config {
      ipv4 {
        address = trimspace(var.ip_address) != "" ? "${var.ip_address}/${tonumber(split("/", var.network_cidr)[1])}" : "dhcp"
        gateway = trimspace(var.ip_address) != "" ? var.gateway : null
      }
    }
  }

  cpu {
    cores = var.cpu
    type  = "host"
  }

  dynamic "serial_device" {
    for_each = local.is_debian ? [1] : []
    content {}
  }

  dynamic "vga" {
    for_each = local.is_debian ? [1] : []
    content {
      type = "serial0"
    }
  }

  memory {
    dedicated = var.memory_mb
  }

  network_device {
    bridge  = var.bridge
    vlan_id = var.vlan
  }

  disk {
    datastore_id = "local-lvm"
    file_id      = local.cloud_init_image_is_import ? null : var.cloud_init_image
    import_from  = local.cloud_init_image_is_import ? var.cloud_init_image : null
    interface    = "scsi0"
    size         = var.disk_gb
  }
}
`

const standaloneContainerTerraformMainTF = `terraform {
  required_version = ">= 1.6.0"

  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.66.0"
    }
  }
}

provider "proxmox" {
  ssh {
    agent = false
{{ .SSHNodeBlocks }}
  }
}

variable "name" {}
variable "proxmox_node_name" {}
variable "cpu" { type = number }
variable "memory_mb" { type = number }
variable "disk_gb" { type = number }
variable "network_cidr" {}
variable "gateway" {}
variable "dns" { type = list(string) }
variable "bridge" {}
variable "vlan" { type = number }
variable "template_image" {}
variable "os_type" {}
variable "ssh_public_key" {
  type    = string
  default = ""
}
variable "root_password" {
  type      = string
  sensitive = true
}
variable "ip_address" {
  type    = string
  default = ""
}
variable "unprivileged" { type = bool }
variable "nesting" { type = bool }

resource "proxmox_virtual_environment_container" "container" {
  node_name    = var.proxmox_node_name
  description  = "Managed by portal"
  unprivileged = var.unprivileged
  start_on_boot = true
  started       = true

  initialization {
    hostname = var.name

    dns {
      servers = var.dns
    }

    ip_config {
      ipv4 {
        address = trimspace(var.ip_address) != "" ? "${var.ip_address}/${tonumber(split("/", var.network_cidr)[1])}" : "dhcp"
        gateway = trimspace(var.ip_address) != "" ? var.gateway : null
      }
    }

    user_account {
      password = var.root_password
      keys     = trimspace(var.ssh_public_key) != "" ? [var.ssh_public_key] : []
    }
  }

  cpu {
    cores = var.cpu
  }

  memory {
    dedicated = var.memory_mb
  }

  disk {
    datastore_id = "local-lvm"
    size         = var.disk_gb
  }

  network_interface {
    name    = "veth0"
    bridge  = var.bridge
    vlan_id = var.vlan
  }

  operating_system {
    template_file_id = var.template_image
    type             = var.os_type
  }

  features {
    nesting = var.nesting
  }
}
`

const standaloneVPSCloudInitTemplate = `#cloud-config
hostname: {{ .Name }}
manage_etc_hosts: true
ssh_pwauth: {{ if .RootPassword }}true{{ else }}false{{ end }}
disable_root: false
users:
  - name: {{ .SSHUser }}
    lock_passwd: false
    shell: /bin/bash
{{- if .SSHPublicKey }}
    ssh_authorized_keys:
      - {{ .SSHPublicKey }}
{{- if .CustomerPublicKey }}
      - {{ .CustomerPublicKey }}
{{- end }}
{{- end }}
{{- if .RootPassword }}
chpasswd:
  expire: false
  list: |
    root:{{ .RootPassword }}
{{- end }}
package_update: true
packages:
  - qemu-guest-agent
  - curl

runcmd:
  - [ bash, -lc, "if [ -f /etc/qemu/qemu-ga.conf ]; then sed -i -E '/^[[:space:]]*(allow-rpcs|block-rpcs|blacklist)[[:space:]]*=/d' /etc/qemu/qemu-ga.conf; fi" ]
  - [ bash, -lc, "for f in /etc/default/qemu-guest-agent /etc/sysconfig/qemu-guest-agent /etc/sysconfig/qemu-ga; do [ -f \"$f\" ] && sed -i -E 's/--(allow-rpcs|block-rpcs|blacklist)(=|[[:space:]])[^[:space:]\"'\"'\"']+//g; s/(allow-rpcs|block-rpcs|blacklist)=['\"'\"'\"']?[^[:space:]\"'\"'\"']+['\"'\"'\"']?//g' \"$f\"; done" ]
  - [ bash, -lc, "install -d -m 0755 /etc/systemd/system/qemu-guest-agent.service.d && qga_bin=$(command -v qemu-ga || true) && if [ -n \"$qga_bin\" ]; then printf '[Service]\nExecStart=\nExecStart=%s -m virtio-serial -p /dev/virtio-ports/org.qemu.guest_agent.0\n' \"$qga_bin\" > /etc/systemd/system/qemu-guest-agent.service.d/10-proxacloud-guest-exec.conf; fi" ]
  - [ systemctl, daemon-reload ]
  - [ systemctl, enable, --now, qemu-guest-agent ]
{{- if .RootPassword }}
  - [ bash, -lc, "install -d -m 0755 /etc/ssh/sshd_config.d && printf 'PasswordAuthentication yes\nPermitRootLogin yes\nKbdInteractiveAuthentication yes\n' > /etc/ssh/sshd_config.d/99-proxacloud-password-auth.conf" ]
  - [ bash, -lc, "systemctl restart sshd || systemctl restart ssh || true" ]
{{- end }}
  - [ bash, -lc, "echo VPS_NAME={{ .Name }} > /etc/managed-vps.env" ]
  - [ bash, -lc, "echo VPS_IP={{ .IPAddress }} >> /etc/managed-vps.env" ]
{{- if .AgentInstallCommand }}
  - [ bash, -lc, {{ printf "%q" .AgentInstallCommand }} ]
{{- end }}
`
