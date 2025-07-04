// macvmagt/internal/utils/vmutils.go (Conceptual with Tart)
package utils

import (
	"encoding/json" // For parsing tart list output
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/changty97/macvmagt/internal/models"
)

// TartVMInfo represents a simplified structure for parsing `tart list --json` output.
// Adjust fields based on actual `tart` output.
type TartVMInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	IP     string `json:"ip"`
	Uptime int64  `json:"uptime"` // Uptime in seconds
	// Tart does not directly expose the base image name in `list --json` output.
	// You might need to track this internally in your vmgr.Manager or imagemgr.Manager.
	// For now, we'll set it to "unknown" or derive it if possible.
}

// GetRunningVMs uses `tart list --json` to get details of running VMs.
func GetRunningVMs() ([]models.VMInfo, error) {
	output, err := ExecuteCommand("tart", "list", "--format", "json")
	if err != nil {
		// Tart list might return an error if no VMs, or empty JSON array
		if strings.Contains(err.Error(), "no VMs found") || strings.TrimSpace(output) == "[]" || strings.Contains(err.Error(), "exit status 1") {
			return []models.VMInfo{}, nil
		}
		return nil, fmt.Errorf("failed to list VMs with tart: %w", err)
	}

	var tartVMs []TartVMInfo
	if err := json.Unmarshal([]byte(output), &tartVMs); err != nil {
		return nil, fmt.Errorf("failed to parse tart list JSON output: %w", err)
	}

	var vms []models.VMInfo
	for _, tvm := range tartVMs {
		if tvm.State == "Running" {
			vms = append(vms, models.VMInfo{
				VMID:           tvm.Name,
				ImageName:      "unknown", // Tart doesn't directly expose base image name in `list --json`.
				RuntimeSeconds: tvm.Uptime,
				VMHostname:     "unknown", // Tart doesn't directly expose hostname. May need SSH to get it.
				VMIPAddress:    tvm.IP,
			})
		}
	}
	return vms, nil
}

// CreateVM creates a new virtual machine using the specified image via `tart`.
// Assumes `imageName` corresponds to a base image known to tart (e.g., `tart pull` has been run).
// `imagePath` is no longer directly used for cloning, but `imageName` is the key for tart.
func CreateVM(vmID, imageName string) error {
	log.Printf("Creating VM %s from tart base image %s...", vmID, imageName)

	// Clone the base image to create a new VM instance.
	// This command creates a new VM based on an existing base image.
	// You might need to add more arguments for CPU, memory, disk size, etc.
	// Example: tart clone <base_image_name> <new_vm_name> --cpu 2 --memory 4GB --disk 50GB
	_, err := ExecuteCommand("tart", "clone", imageName, vmID)
	if err != nil {
		return fmt.Errorf("failed to clone VM %s from image %s using tart: %w", vmID, imageName, err)
	}
	log.Printf("VM %s cloned from image %s.", vmID, imageName)

	// Start the VM.
	// This command runs the cloned VM.
	_, err = ExecuteCommand("tart", "run", vmID)
	if err != nil {
		return fmt.Errorf("failed to start VM %s using tart: %w", vmID, err)
	}
	log.Printf("VM %s started.", vmID)

	// Simulate VM creation/boot time if needed for testing, otherwise remove.
	time.Sleep(10 * time.Second)

	return nil
}

// DeleteVM stops and deletes a virtual machine using `tart`.
func DeleteVM(vmID string) error {
	log.Printf("Deleting VM %s using tart...", vmID)
	// Stop the VM first (tart stop is idempotent, won't error if not running)
	_, err := ExecuteCommand("tart", "stop", vmID)
	if err != nil {
		log.Printf("Warning: Failed to stop VM %s (might not be running or other error): %v", vmID, err)
	}

	// Delete the VM
	_, err = ExecuteCommand("tart", "delete", vmID)
	if err != nil {
		return fmt.Errorf("failed to delete VM %s using tart: %w", vmID, err)
	}
	log.Printf("VM %s deleted successfully.", vmID)
	return nil
}
