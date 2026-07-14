package terraform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactTerraformProgressOutputPreservesShortOutput(t *testing.T) {
	output := "Terraform is refreshing state"

	if got := compactTerraformProgressOutput(output); got != output {
		t.Fatalf("compactTerraformProgressOutput() = %q, want %q", got, output)
	}
}

func TestCompactTerraformProgressOutputKeepsLatestOutputWhenTruncated(t *testing.T) {
	output := strings.Repeat("a", maxTerraformProgressOutputLength) + "latest"

	got := compactTerraformProgressOutput(output)
	if len(got) != maxTerraformProgressOutputLength {
		t.Fatalf("compactTerraformProgressOutput() length = %d, want %d", len(got), maxTerraformProgressOutputLength)
	}
	if !strings.HasSuffix(got, "latest") {
		t.Fatalf("compactTerraformProgressOutput() = %q, want latest output preserved", got)
	}
}

func TestImgImportVolumeParts(t *testing.T) {
	storage, filename, ok := imgImportVolumeParts("cloud-images:import/ubuntu-24.04-server-cloudimg-amd64.img")
	if !ok || storage != "cloud-images" || filename != "ubuntu-24.04-server-cloudimg-amd64.img" {
		t.Fatalf("imgImportVolumeParts() = %q, %q, %t", storage, filename, ok)
	}
	for _, volumeID := range []string{
		"cloud-images:import/ubuntu-24.04.qcow2",
		"cloud-images:iso/ubuntu-24.04.img",
		"cloud-images:import/subdir/ubuntu-24.04.img",
	} {
		if _, _, ok := imgImportVolumeParts(volumeID); ok {
			t.Fatalf("imgImportVolumeParts(%q) unexpectedly accepted", volumeID)
		}
	}
}

func TestRedactTerraformSecrets(t *testing.T) {
	output := "install --token pct_2y-T6Jpp2vuvkp0-LgATRXl8C7CKDCOC5uskwMVIHoI --done"
	got := redactTerraformSecrets(output)
	if strings.Contains(got, "2y-T6Jpp") || got != "install --token pct_[REDACTED] --done" {
		t.Fatalf("redactTerraformSecrets() = %q", got)
	}
}

func TestPrepareSnippetStorageCommandSupportsOlderPVE(t *testing.T) {
	if strings.Contains(prepareSnippetStorageCommand, "pvesm config") {
		t.Fatal("snippet preflight uses unsupported pvesm config command")
	}
	for _, want := range []string{"pvesh get /storage/local", "mkdir -p", "pvesm set local --content"} {
		if !strings.Contains(prepareSnippetStorageCommand, want) {
			t.Fatalf("snippet preflight missing %q", want)
		}
	}
}

func TestPrepareStandaloneVPSWorkspaceIncludesDebianWorkaround(t *testing.T) {
	runner := NewRunner("terraform", t.TempDir(), "")

	workDir, err := runner.PrepareStandaloneVPSWorkspace(StandaloneVPSSpec{
		WorkspaceKey:      "debian-vps",
		Name:              "debian-vps",
		ProxmoxNodeName:   "pve01",
		CloudInitImage:    "cloud-images:import/debian-12-genericcloud-amd64.qcow2",
		OSType:            "debian",
		RootPassword:      "ChangeMe123!",
		SSHPublicKey:      "ssh-ed25519 AAAA-automation",
		CustomerPublicKey: "ssh-ed25519 AAAA-customer",
		NetworkCIDR:       "192.168.10.0/24",
		Gateway:           "192.168.10.1",
		Bridge:            "vmbr0",
		DNS:               []string{"1.1.1.1"},
	})
	if err != nil {
		t.Fatalf("PrepareStandaloneVPSWorkspace() error = %v", err)
	}

	mainTF := readTestFile(t, filepath.Join(workDir, "main.tf"))
	tfvars := readTestFile(t, filepath.Join(workDir, "terraform.tfvars.json"))
	for _, want := range []string{
		`interface         = local.is_debian ? "scsi1" : null`,
		`dynamic "serial_device"`,
		`dynamic "vga"`,
		`type = "serial0"`,
	} {
		if !strings.Contains(mainTF, want) {
			t.Fatalf("main.tf missing %q", want)
		}
	}
	if !strings.Contains(tfvars, `"os_type": "debian"`) {
		t.Fatalf("terraform.tfvars.json = %s, want debian os_type", tfvars)
	}
	cloudInit := readTestFile(t, filepath.Join(workDir, "cloud-init", "user-data.yaml"))
	for _, key := range []string{"AAAA-automation", "AAAA-customer"} {
		if !strings.Contains(cloudInit, key) {
			t.Fatalf("VPS cloud-init missing %s", key)
		}
	}
	for _, want := range []string{"disable_root: false", "name: root"} {
		if !strings.Contains(cloudInit, want) {
			t.Fatalf("VPS cloud-init missing %q", want)
		}
	}
}

func TestPrepareStandaloneVPSWorkspacePreservesNonDebianOSType(t *testing.T) {
	runner := NewRunner("terraform", t.TempDir(), "")

	workDir, err := runner.PrepareStandaloneVPSWorkspace(StandaloneVPSSpec{
		WorkspaceKey:    "rocky-vps",
		Name:            "rocky-vps",
		ProxmoxNodeName: "pve01",
		CloudInitImage:  "cloud-images:import/rocky-9.qcow2",
		OSType:          "rocky",
		RootPassword:    "ChangeMe123!",
		NetworkCIDR:     "192.168.10.0/24",
		Gateway:         "192.168.10.1",
		Bridge:          "vmbr0",
		DNS:             []string{"1.1.1.1"},
	})
	if err != nil {
		t.Fatalf("PrepareStandaloneVPSWorkspace() error = %v", err)
	}

	tfvars := readTestFile(t, filepath.Join(workDir, "terraform.tfvars.json"))
	if !strings.Contains(tfvars, `"os_type": "rocky"`) {
		t.Fatalf("terraform.tfvars.json = %s, want rocky os_type", tfvars)
	}
}

func TestPrepareClusterWorkspaceImportsSharedImageAndInstallsTunnelAgent(t *testing.T) {
	runner := NewRunner("terraform", t.TempDir(), "")
	workDir, err := runner.PrepareWorkspace(WorkspaceSpec{
		ClusterName: "demo", UbuntuImage: "cloud-images:import/ubuntu-24.04.qcow2", SSHPublicKey: "ssh-ed25519 AAAA-automation", CustomerPublicKey: "ssh-ed25519 AAAA-customer",
		NetworkCIDR: "192.168.10.0/24", Gateway: "192.168.10.1", Bridge: "vmbr0",
		Nodes: []VMNode{{VMName: "demo-cp-1", Role: "control-plane", RoleIndex: 1, ProxmoxNodeName: "pve01", CPU: 2, MemoryMB: 2048, DiskGB: 40, IPAddress: "192.168.10.10", NetworkCIDR: "192.168.10.0/24", Gateway: "192.168.10.1", Bridge: "vmbr0", AgentInstallCommand: "install-agent --token secret"}},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace() error = %v", err)
	}
	mainTF := readTestFile(t, filepath.Join(workDir, "main.tf"))
	if !strings.Contains(mainTF, `import_from  = local.ubuntu_image_is_import ? var.ubuntu_image : null`) {
		t.Fatalf("main.tf does not import shared images")
	}
	if !strings.Contains(mainTF, `data      = sensitive(file("${path.module}/cloud-init/${each.value.vm_name}.yaml"))`) {
		t.Fatalf("main.tf does not redact cloud-init secrets from Terraform output")
	}
	cloudInit := readTestFile(t, filepath.Join(workDir, "cloud-init", "demo-cp-1.yaml"))
	if !strings.Contains(cloudInit, "install-agent --token secret") {
		t.Fatalf("cloud-init does not include tunnel agent command")
	}
	for _, key := range []string{"AAAA-automation", "AAAA-customer"} {
		if !strings.Contains(cloudInit, key) {
			t.Fatalf("cluster cloud-init missing %s", key)
		}
	}
	for _, want := range []string{"disable_root: false", "name: root"} {
		if !strings.Contains(cloudInit, want) {
			t.Fatalf("cluster cloud-init missing %q", want)
		}
	}
	if strings.Contains(cloudInit, "name: ubuntu") {
		t.Fatal("cluster cloud-init still creates the ubuntu login user")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
