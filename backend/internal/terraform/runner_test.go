package terraform

import (
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
