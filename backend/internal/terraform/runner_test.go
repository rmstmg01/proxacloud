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

func TestPrepareStandaloneVPSWorkspaceIncludesDebianWorkaround(t *testing.T) {
	runner := NewRunner("terraform", t.TempDir(), "")

	workDir, err := runner.PrepareStandaloneVPSWorkspace(StandaloneVPSSpec{
		WorkspaceKey:    "debian-vps",
		Name:            "debian-vps",
		ProxmoxNodeName: "pve01",
		CloudInitImage:  "cloud-images:import/debian-12-genericcloud-amd64.qcow2",
		OSType:          "debian",
		RootPassword:    "ChangeMe123!",
		NetworkCIDR:     "192.168.10.0/24",
		Gateway:         "192.168.10.1",
		Bridge:          "vmbr0",
		DNS:             []string{"1.1.1.1"},
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

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
