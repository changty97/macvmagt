package vmgr

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/utils" // Corrected import path
)

// VMManager handles creating, deleting, and managing VM instances.
type VMManager struct {
	cfg        *config.Config
	runningVMs sync.Map // map[vmID]*models.VMInfo
	vmBasePath string   // Base path for VM data (e.g., /var/macvmorx/vms)
	sshPort    string   // Default SSH port for VMs (tart typically uses 2222)
}

// NewVMManager creates and initializes a new VMManager.
func NewVMManager(cfg *config.Config) (*VMManager, error) {
	vmBasePath := "/var/macvmorx/vms" // Default for tart VMs
	if err := os.MkdirAll(vmBasePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create VM base directory %s: %w", vmBasePath, err)
	}

	return &VMManager{
		cfg:        cfg,
		vmBasePath: vmBasePath,
		sshPort:    "2222", // Tart's default SSH port
	}, nil
}

// ProvisionVM creates a new VM, installs the GitHub runner, and starts it.
func (vmm *VMManager) ProvisionVM(ctx context.Context, cmd models.VMProvisionCommand, imagePath string) (*models.VMInfo, error) {
	vmID := cmd.VMID
	runnerName := cmd.RunnerName
	runnerRegistrationToken := cmd.RunnerRegistrationToken
	runnerLabels := strings.Join(cmd.RunnerLabels, ",") // Labels are joined into a single string

	log.Printf("Provisioning VM '%s' from image '%s'...", vmID, imagePath)

	// 1. Check if VM already exists (e.g., if a previous attempt failed mid-way)
	_, err := os.Stat(filepath.Join(vmm.vmBasePath, vmID))
	if err == nil {
		log.Printf("VM directory for '%s' already exists. Attempting to delete and recreate.", vmID)
		if _, _, delErr := utils.RunCommand("tart", "delete", vmID); delErr != nil {
			log.Printf("Warning: Failed to delete existing VM '%s': %v", vmID, delErr)
			// Proceeding might lead to issues, but for robustness, we try to recreate.
		}
	}

	// 2. Create the VM using `tart create`
	log.Printf("Running tart create for VM '%s' from image '%s'...", vmID, imagePath)
	stdout, stderr, err := utils.RunCommand("tart", "create", vmID, "--from-ipsw", imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM '%s' with tart: %w, stdout: %s, stderr: %s", vmID, err, stdout, stderr)
	}
	log.Printf("Tart create output for VM '%s':\nStdout: %s\nStderr: %s", vmID, stdout, stderr)

	// 3. Start the VM using `tart run` in background
	log.Printf("Running tart run for VM '%s' in background...", vmID)
	// Execute tart run in a separate command that is detached.
	cmdRun := exec.Command("nohup", "tart", "run", vmID, "&>/dev/null &")
	err = cmdRun.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start VM '%s' with tart run (nohup): %w", vmID, err)
	}
	log.Printf("VM '%s' started with tart run (PID: %d).", vmID, cmdRun.Process.Pid)

	// 4. Get VM IP Address and wait for SSH
	var vmIP string
	maxAttempts := 30 // Try for up to 30 * 2 seconds = 1 minute
	for i := 0; i < maxAttempts; i++ {
		vmIP, err = utils.GetVMIPAddress(vmID)
		if err == nil && vmIP != "" {
			log.Printf("Discovered VM IP for '%s': %s", vmID, vmIP)
			break
		}
		log.Printf("Waiting for VM '%s' IP address... Attempt %d/%d", vmID, i+1, maxAttempts)
		time.Sleep(2 * time.Second) // Wait before retrying
	}
	if vmIP == "" {
		return nil, fmt.Errorf("failed to get IP address for VM '%s' after multiple attempts: %w", vmID, err)
	}

	// Wait for SSH to be ready on the VM
	err = utils.WaitForSSHReady(vmIP, vmm.sshPort, vmm.cfg.VMSSHUser, vmm.cfg.VMSSHKeyPath, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("SSH not ready on VM '%s' (%s): %w", vmID, vmIP, err)
	}

	// 5. Execute post-provisioning script (GitHub Runner installation)
	log.Printf("Executing GitHub runner installation script on VM '%s' (%s)...", vmID, vmIP)
	runnerScriptContent, err := os.ReadFile(vmm.cfg.GitHubRunnerScriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read GitHub runner script template: %w", err)
	}

	// Replace placeholders in the script template
	script := string(runnerScriptContent)
	// These placeholders in the script template will be replaced by the agent
	// before sending the script to the VM.
	// You should configure these values in your agent's environment or config.
	script = strings.ReplaceAll(script, "YOUR_GITHUB_ORG_OR_USER_ACTUAL", "your-github-org-or-user") // Placeholder
	script = strings.ReplaceAll(script, "YOUR_GITHUB_REPO_ACTUAL", "your-github-repo")               // Placeholder
	script = strings.ReplaceAll(script, "YOUR_GITHUB_RUNNER_REGISTRATION_TOKEN", runnerRegistrationToken)

	// Send the script to the VM and execute it, passing runnerName and runnerLabels as arguments
	// The `bash -s --` syntax is crucial to correctly pass arguments to the script read from stdin.
	sshCommand := fmt.Sprintf("bash -s -- '%s' '%s' <<'EOF'\n%s\nEOF", runnerName, runnerLabels, script)
	sshStdout, sshStderr, err := utils.ExecuteSSHCommand(vmIP, vmm.sshPort, vmm.cfg.VMSSHUser, vmm.cfg.VMSSHKeyPath, sshCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to execute GitHub runner script on VM '%s' (%s): %w, stdout: %s, stderr: %s",
			vmID, vmIP, err, sshStdout, sshStderr)
	}
	log.Printf("GitHub runner script executed successfully on VM '%s'. Stdout: %s", vmID, sshStdout)

	now := time.Now()
	vmInfo := &models.VMInfo{
		VMID:        vmID,
		ImageName:   cmd.ImageName,
		VMHostname:  vmID, // Assuming VMID acts as hostname for now
		VMIPAddress: vmIP,
		VMStartTime: &now, // Set the VM start time
		// RuntimeSeconds will be calculated by heartbeat sender
	}
	vmm.runningVMs.Store(vmID, vmInfo)
	log.Printf("VM '%s' provisioned and GitHub runner configured.", vmID)

	return vmInfo, nil
}

// DeleteVM stops and deletes a VM.
func (vmm *VMManager) DeleteVM(ctx context.Context, cmd models.VMDeleteCommand) error {
	vmID := cmd.VMID
	log.Printf("Deleting VM '%s'...", vmID)

	// 1. Stop the VM (if running) and delete using `tart delete`
	// `tart delete` handles stopping the VM process associated with the VMID.
	stdout, stderr, err := utils.RunCommand("tart", "delete", vmID)
	if err != nil {
		return fmt.Errorf("failed to delete VM '%s' with tart: %w, stdout: %s, stderr: %s", vmID, err, stdout, stderr)
	}
	log.Printf("Tart delete output for VM '%s':\nStdout: %s\nStderr: %s", vmID, stdout, stderr)

	vmm.runningVMs.Delete(vmID)
	log.Printf("VM '%s' deleted successfully.", vmID)
	return nil
}

// GetRunningVMs returns a list of currently running VMs.
func (vmm *VMManager) GetRunningVMs() []models.VMInfo {
	var vms []models.VMInfo
	vmm.runningVMs.Range(func(key, value interface{}) bool {
		vm := value.(*models.VMInfo)
		// Calculate runtime for reporting in heartbeat
		if vm.VMStartTime != nil {
			vm.RuntimeSeconds = int64(time.Since(*vm.VMStartTime).Seconds())
		} else {
			vm.RuntimeSeconds = 0 // Or some other default if start time is unknown
		}
		vms = append(vms, *vm)
		return true
	})
	return vms
}
