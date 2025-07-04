package vmgr

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/imagemgr"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/utils"
)

// Manager handles VM creation, deletion, and status.
type Manager struct {
	cfg          *config.Config
	imageManager *imagemgr.Manager
	// Add a mutex if VM operations need to be synchronized
	// activeVMs sync.Map // Map[string]*models.VMInfo if agent needs to track internal VM state
}

// NewManager creates a new VM Manager.
func NewManager(cfg *config.Config, im *imagemgr.Manager) *Manager {
	return &Manager{
		cfg:          cfg,
		imageManager: im,
	}
}

// ProvisionVM handles the request to provision a new VM.
// This is the core logic for spinning up a VM for a GitHub runner.
func (m *Manager) ProvisionVM(cmd models.VMProvisionCommand) error {
	log.Printf("Received request to provision VM %s with image %s", cmd.VMID, cmd.ImageName)

	// 1. Check if image is cached and ready
	imagePath, ok := m.imageManager.GetCachedImagePath(cmd.ImageName)
	if !ok {
		// Image not cached, request download
		log.Printf("Image %s not cached. Requesting download.", cmd.ImageName)
		m.imageManager.RequestImageDownload(cmd.ImageName)

		// Wait for download to complete (non-blocking for agent, but blocking for this VM provisioning call)
		// This is where the "queue/wait the current GitHub job" logic comes in.
		// The orchestrator would have already decided this node is suitable for download.
		// Here, we block THIS VM provisioning request until download is done.
		timeout := time.After(30 * time.Minute) // Max wait time for download
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				imagePath, ok = m.imageManager.GetCachedImagePath(cmd.ImageName)
				if ok && !m.imageManager.IsImageDownloading(cmd.ImageName) {
					log.Printf("Image %s downloaded. Proceeding with VM provisioning.", cmd.ImageName)
					goto ImageReady // Break out of loop and continue
				}
				log.Printf("Waiting for image %s to finish downloading...", cmd.ImageName)
			case <-timeout:
				return fmt.Errorf("timeout waiting for image %s to download for VM %s", cmd.ImageName, cmd.VMID)
			}
		}
	ImageReady: // Label to jump to after successful download
		if imagePath == "" {
			return fmt.Errorf("image %s path is empty after download, cannot provision VM %s", cmd.ImageName, cmd.VMID)
		}
	}

	// 2. Create and Start the VM
	// This is where you call macOS `vm` commands or interact with Hypervisor.framework.
	// For ephemeral runners, you'd want to clone the base image to a new location for the VM.
	vmBasePath := fmt.Sprintf("/var/macvmorx/vms/%s", cmd.VMID)
	if err := os.MkdirAll(vmBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create VM base directory %s: %w", vmBasePath, err)
	}

	// Example: Copy the base image to the VM's directory
	vmDiskPath := filepath.Join(vmBasePath, fmt.Sprintf("%s.sparseimage", cmd.VMID))
	log.Printf("Cloning image %s to %s for VM %s...", imagePath, vmDiskPath, cmd.VMID)
	_, err := utils.ExecuteCommand("cp", imagePath, vmDiskPath) // Simple copy, consider `hdiutil compact` for sparse images
	if err != nil {
		return fmt.Errorf("failed to clone VM disk image: %w", err)
	}
	log.Printf("Image cloned for VM %s.", cmd.VMID)

	// Actual VM creation using `vm` command (highly simplified example)
	// This assumes `vm` can create a VM from a disk image directly.
	// For a more robust solution, consider `tart` (https://github.com/cirruslabs/tart)
	// or direct Hypervisor.framework interaction in Swift.
	// Example `vm` command for creating a VM:
	// `vm create --name <VMID> --disk <vmDiskPath> --memory 4G --cpu 2`
	// You'd need to configure networking (e.g., bridged, NAT) and other VM parameters.
	// For simplicity, we'll just simulate the creation.
	log.Printf("Placeholder: Executing VM creation command for %s using disk %s...", cmd.VMID, vmDiskPath)
	// Simulate VM creation time
	time.Sleep(10 * time.Second) // Simulate actual VM creation/boot time

	// Start the VM
	// `vm start <VMID>`
	log.Printf("Placeholder: VM %s started.", cmd.VMID)

	// 3. Run Post-Script to Install GitHub Runner
	// This script should be located on the Mac Mini agent.
	// It needs to be executed *inside* the newly created VM. This is complex
	// and typically involves SSH or a shared folder mechanism.
	// For now, we'll simulate it.
	// runnerScriptPath := "github.com/changty97/macvmagt/scripts/install_github_runner.sh.template" // Adjust path
	uniqueRunnerName := fmt.Sprintf("macvmorx-runner-%s-%s", m.cfg.NodeID, cmd.VMID)

	log.Printf("Placeholder: Running post-script to install GitHub runner '%s' on VM %s...", uniqueRunnerName, cmd.VMID)
	// Example: Execute a script via SSH into the VM (requires SSH server in VM and credentials)
	// `ssh user@<VM_IP_ADDRESS> "bash -s" < ${runnerScriptPath} ${uniqueRunnerName}`
	// Or use a shared folder and execute locally within the VM.
	time.Sleep(5 * time.Second) // Simulate runner installation
	log.Printf("Placeholder: GitHub runner '%s' installed on VM %s.", uniqueRunnerName, cmd.VMID)

	log.Printf("VM %s provisioned and ready for GitHub job.", cmd.VMID)
	return nil
}

// DeleteVM handles the request to delete a VM.
func (m *Manager) DeleteVM(cmd models.VMDeleteCommand) error {
	log.Printf("Received request to delete VM %s", cmd.VMID)

	// 1. Stop and Delete the VM
	// This calls the vmutils.DeleteVM which uses the `vm` command.
	err := utils.DeleteVM(cmd.VMID)
	if err != nil {
		return fmt.Errorf("failed to delete VM %s: %w", cmd.VMID, err)
	}

	// 2. Clean up VM's disk image and directory
	vmBasePath := fmt.Sprintf("/var/macvmorx/vms/%s", cmd.VMID)
	log.Printf("Cleaning up VM directory: %s", vmBasePath)
	if err := os.RemoveAll(vmBasePath); err != nil {
		log.Printf("Warning: Failed to remove VM directory %s: %v", vmBasePath, err)
	}

	log.Printf("VM %s deleted and cleaned up.", cmd.VMID)
	return nil
}
