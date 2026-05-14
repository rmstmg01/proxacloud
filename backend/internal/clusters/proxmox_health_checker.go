package clusters

import (
	"fmt"
	"log"
)

// ProxmoxHealthChecker implements the HealthChecker interface for Proxmox
type ProxmoxHealthChecker struct {
	proxmoxSvc ProxmoxCredentialStore
}

// NewProxmoxHealthChecker creates a new Proxmox-based health checker
func NewProxmoxHealthChecker(proxmoxSvc ProxmoxCredentialStore) *ProxmoxHealthChecker {
	return &ProxmoxHealthChecker{
		proxmoxSvc: proxmoxSvc,
	}
}

// CheckNodeHealth checks if a VM exists and is accessible on Proxmox
// Currently returns unknown since the Proxmox client doesn't have VM-level APIs
// Future: extend this to query actual VM status from Proxmox /api2/json/nodes/{node}/qemu endpoint
func (phc *ProxmoxHealthChecker) CheckNodeHealth(credentialID int64, vmName string, proxmoxNodeName string) (bool, error) {
	if credentialID == 0 {
		return false, fmt.Errorf("invalid credential id: %d", credentialID)
	}

	// TODO: Extend Proxmox client to support VM status checking
	// For now, we'll implement a basic health check that assumes nodes are healthy
	// if the credential is valid. Future enhancement: query /api2/json/nodes/{node}/qemu/{vmid}/status/current

	// Verify the credential exists
	_, err := phc.proxmoxSvc.GetCredentialWithSecrets(credentialID)
	if err != nil {
		log.Printf("[ProxmoxHealthChecker] Error loading credential %d: %v", credentialID, err)
		return false, nil // Credential doesn't exist = node unhealthy
	}

	// For now, we'll return true if credential is valid
	// In production, you should extend the Proxmox client to query actual VM status
	log.Printf("[ProxmoxHealthChecker] Credential %d for node %s is valid", credentialID, vmName)

	// TODO: Add actual VM status check once Proxmox client is enhanced
	// vm, err := client.GetVMStatus(ctx, proxmoxNodeName, vmName)
	// if err != nil { return false, nil }
	// isHealthy := vm.Status == "running"

	return true, nil // Assume healthy for now - extend this in the future
}
