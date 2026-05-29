package clusters

import (
	"fmt"
	"log"
	"strings"
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

// CheckNodeHealth checks if a VM exists and is running on Proxmox.
func (phc *ProxmoxHealthChecker) CheckNodeHealth(credentialID int64, vmName string, proxmoxNodeName string) (string, error) {
	if credentialID == 0 {
		return "unknown", fmt.Errorf("invalid credential id: %d", credentialID)
	}

	cred, err := phc.proxmoxSvc.GetCredentialWithSecrets(credentialID)
	if err != nil {
		log.Printf("[ProxmoxHealthChecker] Error loading credential %d: %v", credentialID, err)
		return "unknown", err
	}

	status, err := phc.proxmoxSvc.GetVMStatusByName(cred, proxmoxNodeName, vmName)
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if strings.Contains(lowerErr, "not found") {
			return "missing", nil
		}
		log.Printf("[ProxmoxHealthChecker] Error checking VM %s on %s: %v", vmName, proxmoxNodeName, err)
		return "unknown", err
	}

	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "healthy", nil
	case "stopped", "paused", "suspended":
		return "unhealthy", nil
	default:
		return "unknown", nil
	}
}
