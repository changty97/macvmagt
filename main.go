package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

// --- Configuration and Constants ---

const (
	// Default base path where macosvm stores its VM files (e.g., /opt/macosvm/vms).
	// This might need to be adjusted based on the actual macosvm configuration.
	vmBasePath = "/opt/macosvm/vms"
	// The path where the macosvm binary is expected to be after extraction.
	// We use the full path to avoid relying on the agent's PATH environment variable.
	macosvmBinaryPath = "/usr/local/bin/macosvm"
	// Installation URL provided by the user.
	macosvmInstallURL = "https://github.com/s-u/macosvm/releases/download/0.2-1/macosvm-0.2-1-arm64-darwin21.tar.gz"
)

// VMProvisionCommand represents a command from the orchestrator to provision a VM.
// This structure is preserved to align with the orchestrator's expectations.
type VMProvisionCommand struct {
	VMID                    string   `json:"vmId"`                    // Unique ID for the new VM
	ImageName               string   `json:"imageName"`               // Image to use (e.g., "ventura" or "my-golden-image")
	RunnerRegistrationToken string   `json:"runnerRegistrationToken"` // GitHub Actions runner registration token
	RunnerName              string   `json:"runnerName"`              // Unique name for the GitHub runner
	RunnerLabels            []string `json:"runnerLabels"`            // Labels for the GitHub runner
	// LocalImagePath is critical for macosvm, as it points to the macOS disk image file.
	LocalImagePath string `json:"localImagePath"`
}

// --- Utility Functions ---

// runCommand executes a shell command and returns stdout, stderr, and an error.
func runCommand(name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	log.Printf("Executing: %s %s", name, strings.Join(args, " "))
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// checkAndInstallMacosvm checks if the macosvm binary exists and installs it if not.
func checkAndInstallMacosvm() error {
	// First check: Is the binary at the expected path?
	_, err := os.Stat(macosvmBinaryPath)
	if err == nil {
		log.Printf("macosvm binary found at %s. Skipping installation.", macosvmBinaryPath)
		return nil
	}

	log.Printf("macosvm binary not found. Attempting installation from: %s", macosvmInstallURL)

	// Step 1: Install using curl and tar pipe
	// NOTE: This installation method may require the agent to run with sufficient permissions
	// (e.g., root/sudo privileges to place the binary in /usr/local/bin,
	// or the agent needs to be pre-authorized to use the binary if it's placed elsewhere).
	// installCmd := fmt.Sprintf("curl -L %s | tar -C /usr/local/bin -xzf - macosvm", macosvmInstallURL)

	// We use a different installation approach here: curl downloads to stdout, which is piped to tar.
	// Since the agent binary is expected to be placed in /usr/local/bin, we attempt to put it there directly.

	// Create the directory if it doesn't exist (assuming /usr/local/bin is writeable or using sudo)
	if err := os.MkdirAll(filepath.Dir(macosvmBinaryPath), 0755); err != nil {
		log.Printf("Warning: Failed to create directory for macosvm: %v", err)
		// This might fail if agent doesn't have privileges, but we proceed.
	}

	// Execute the install command using bash to handle the pipe
	// Note: The installation command provided in the prompt extracts to the current directory.
	// To ensure the binary lands in a runnable location, we'll use a slightly safer temp extraction:
	tempDir, err := ioutil.TempDir("", "macosvm-install")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory

	// Execute the install command: curl and tar vxz into the temp directory
	cmd := exec.Command("/bin/bash", "-c", fmt.Sprintf("curl -L %s | tar -C %s -xzf -", macosvmInstallURL, tempDir))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Installation failed. Stdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
		return fmt.Errorf("failed to extract macosvm: %w, stderr: %s", err, stderr.String())
	}

	// Move the extracted binary to the final path (requires appropriate permissions)
	extractedPath := filepath.Join(tempDir, "macosvm") // Assuming the binary is named 'macosvm' inside the tar
	if _, err := os.Stat(extractedPath); os.IsNotExist(err) {
		// Try macosvm-0.2-1-arm64-darwin21/macosvm if the archive contained a folder
		extractedPath = filepath.Join(tempDir, "macosvm-0.2-1-arm64-darwin21", "macosvm")
	}

	if _, err := os.Stat(extractedPath); os.IsNotExist(err) {
		return fmt.Errorf("failed to find 'macosvm' binary inside extracted archive at %s or %s", filepath.Join(tempDir, "macosvm"), filepath.Join(tempDir, "macosvm-0.2-1-arm64-darwin21", "macosvm"))
	}

	if err := os.Rename(extractedPath, macosvmBinaryPath); err != nil {
		// If rename fails, it's likely a permission issue. Log and fail.
		return fmt.Errorf("failed to move macosvm to %s (Permissions issue?): %w", macosvmBinaryPath, err)
	}

	// Make the binary executable
	if err := os.Chmod(macosvmBinaryPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permission on macosvm: %w", err)
	}

	log.Println("macosvm installation successful.")
	return nil
}

// --- Handler Functions ---

// handleProvisionVM receives a command to provision a VM using macosvm.
func handleProvisionVM(w http.ResponseWriter, r *http.Request) {
	log.Println("Received ProvisionVM request.")

	if err := checkAndInstallMacosvm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to install or locate macosvm: %v", err), http.StatusInternalServerError)
		return
	}

	var cmd VMProvisionCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding provision payload: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	vmID := cmd.VMID
	vmDir := filepath.Join(vmBasePath, vmID)

	// 1. Check if VM already exists (using vm.json file, the macosvm state indicator)
	vmStateFile := filepath.Join(vmDir, "vm.json")
	if _, err := os.Stat(vmStateFile); err == nil {
		log.Printf("VM state file for '%s' already exists. Attempting to delete and recreate.", vmID)

		// Use macosvm delete command for cleanup
		if _, stderr, delErr := runCommand(macosvmBinaryPath, "delete", vmID); delErr != nil {
			log.Printf("Warning: Failed to delete existing VM '%s': %v, stderr: %s", vmID, delErr, stderr)
			// Force removal of the directory if macosvm delete failed (e.g., if VM wasn't running)
			os.RemoveAll(vmDir)
		} else {
			log.Printf("Successfully deleted existing VM '%s'.", vmID)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("Error checking VM state directory: %v", err)
		http.Error(w, "Internal check failed", http.StatusInternalServerError)
		return
	}

	// 2. Image Handling: Ensure the local image path is provided
	diskImagePath := cmd.LocalImagePath
	if diskImagePath == "" {
		log.Printf("Error: LocalImagePath is missing for macosvm. Cannot create VM.")
		http.Error(w, "LocalImagePath (path to macOS disk image) required for macosvm is missing.", http.StatusBadRequest)
		return
	}
	// Check if the disk image exists locally before proceeding
	if _, err := os.Stat(diskImagePath); os.IsNotExist(err) {
		log.Printf("Error: Disk image not found at %s. Ensure image is downloaded from GCP.", diskImagePath)
		http.Error(w, "Local macOS disk image not found at specified path.", http.StatusBadRequest)
		return
	}

	log.Printf("Using image path: %s", diskImagePath)

	// 3. Create the VM using macosvm
	// We hardcode some sensible defaults for an ARC runner.
	createArgs := []string{
		"create",
		"--id", vmID,
		"--disk-image", diskImagePath,
		"--arch", "arm64",
		"--memory", "8G", // Recommended memory for a macOS runner
		"--cpus", "4", // Recommended CPUs
	}

	if stdout, stderr, err := runCommand(macosvmBinaryPath, createArgs...); err != nil {
		log.Printf("VM creation failed for %s. Stderr:\n%s\nStdout:\n%s", vmID, stderr, stdout)
		http.Error(w, fmt.Sprintf("Failed to create VM: %v", stderr), http.StatusInternalServerError)
		return
	}
	log.Printf("Successfully created VM '%s'.", vmID)

	// 4. Start the VM in the background
	// macosvm run automatically starts the VM.
	runArgs := []string{
		"run",
		vmID,
	}

	// Execute run in the background. In a real ARC scenario, the disk image MUST
	// contain a startup script that fetches the RunnerRegistrationToken and starts the ARC runner.
	go func() {
		// Log VM run output in the background
		log.Printf("Attempting to run VM '%s'...", vmID)
		stdout, stderr, err := runCommand(macosvmBinaryPath, runArgs...)
		if err != nil {
			log.Printf("VM Run command failed for %s: %v. Stderr:\n%s\nStdout:\n%s", vmID, err, stderr, stdout)
			// At this point, the orchestrator needs to detect the runner failed to connect.
		} else {
			log.Printf("VM '%s' run command finished (likely VM shut down). Stdout:\n%s", vmID, stdout)
		}
	}()

	// 5. Send Accepted Response
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "VM provisioning initiated successfully. ARC runner should connect shortly.",
		"vmId":    vmID,
		"nodeId":  os.Getenv("MACVM_NODE_ID"), // Assuming NODE_ID is set as an env var
	})
}

// handleDeleteVM receives a command to delete a VM using macosvm.
func handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	log.Println("Received DeleteVM request.")

	if err := checkAndInstallMacosvm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to install or locate macosvm: %v", err), http.StatusInternalServerError)
		return
	}

	var cmd struct {
		VMID string `json:"vmId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding delete payload: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	vmID := cmd.VMID
	log.Printf("Attempting to delete VM: %s", vmID)

	// Use macosvm delete command
	if stdout, stderr, err := runCommand(macosvmBinaryPath, "delete", vmID); err != nil {
		// Check if the error is due to the VM not existing
		if strings.Contains(stderr, "no such VM") {
			log.Printf("VM %s not found. Assuming successfully removed or never existed.", vmID)
		} else {
			log.Printf("VM deletion failed for %s: %v. Stderr:\n%s\nStdout:\n%s", vmID, err, stderr, stdout)
			http.Error(w, fmt.Sprintf("Failed to delete VM: %v", stderr), http.StatusInternalServerError)
			return
		}
	} else {
		log.Printf("Successfully deleted VM: %s", vmID)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "VM deletion confirmed.",
		"vmId":    vmID,
	})
}

// handleGetVms returns a list of existing VM definitions by querying macosvm's state files.
// This serves as a simple local inventory check.
func handleGetVms(w http.ResponseWriter, r *http.Request) {
	if err := checkAndInstallMacosvm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to install or locate macosvm: %v", err), http.StatusInternalServerError)
		return
	}

	// Query macosvm list (assuming it outputs one VM per line, with the ID)
	// A more robust solution would be to use a JSON output if available.
	stdout, stderr, err := runCommand(macosvmBinaryPath, "list")
	if err != nil {
		log.Printf("Error listing VMs with macosvm: %v. Stderr: %s", err, stderr)
		http.Error(w, "Failed to list VMs", http.StatusInternalServerError)
		return
	}

	// Parse the output (assuming output is lines of VM IDs or names)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var vmList []string
	if len(lines) > 0 && lines[0] != "" {
		// Simple filter to remove header or empty lines.
		// Assuming lines contains VM IDs.
		for _, line := range lines {
			if strings.Contains(line, "ID:") {
				// Example: "ID: vm-12345 Name: runner-001 State: running"
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "ID:" && i+1 < len(parts) {
						vmList = append(vmList, parts[i+1])
						break
					}
				}
			} else if !strings.Contains(line, "No VMs found") && !strings.Contains(line, "ID") {
				// Simpler assumption: each non-header/non-empty line is a VM ID (if list output is very basic)
				vmList = append(vmList, line)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodeId":  os.Getenv("MACVM_NODE_ID"),
		"vmCount": len(vmList),
		"vms":     vmList,
		"details": "VM IDs retrieved from 'macosvm list' command.",
	})
}

// main sets up the HTTP server.
func main() {
	// Set up the router and API endpoints
	router := mux.NewRouter()

	// Endpoints used by the orchestrator (or ARC Controller)
	router.HandleFunc("/provision-vm", handleProvisionVM).Methods("POST")
	router.HandleFunc("/delete-vm", handleDeleteVM).Methods("POST")
	router.HandleFunc("/vms", handleGetVms).Methods("GET") // Simple check for current VMs

	// Ensure the macosvm base path exists
	if err := os.MkdirAll(vmBasePath, 0755); err != nil {
		log.Fatalf("Failed to create VM base path %s: %v", vmBasePath, err)
	}

	addr := ":8081"
	log.Printf("MacVM Agent (macosvm-based) starting on %s", addr)

	// Check for macosvm on startup
	if err := checkAndInstallMacosvm(); err != nil {
		log.Printf("WARNING: macosvm installation failed on startup. Will re-attempt on first /provision-vm call: %v", err)
	}

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
