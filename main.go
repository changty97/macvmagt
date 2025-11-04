package main

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gorilla/mux"
)

// --- CONFIGURATION CONSTANTS AND GLOBALS ---
const (
	// Environment Variable Keys
	VMRootDirEnv  = "VM_ROOT_DIR"     // e.g., /Users/arc-user/runners
	ImageCacheEnv = "IMAGE_CACHE_DIR" // e.g., /Users/arc-user/macosv_vm_base_images

	MaxActiveVMs = 1 // Enforce the limit of only one active VM per Mac Mini
)

var (
	vmRootDir     string
	imageCacheDir string
	mutex         sync.Mutex // Mutex to protect concurrent access to the VM directory check/creation
)

// --- DATA STRUCTURES ---

// VMStatus represents the data returned by the /vms endpoint.
type VMStatus struct {
	Details      string   `json:"details"`
	AvailableVMs int      `json:"availableSlots"` // Slots available for deployment
	VMCount      int      `json:"vmCount"`        // Total VMs currently defined (vm_* folders exist)
	VMs          []string `json:"vms"`
	NodeID       string   `json:"nodeId"`
}

// ProvisionVMRequest structure for POST /provision-vm
type ProvisionVMRequest struct {
	VMID                    string   `json:"vmId"`
	ImageName               string   `json:"imageName"`
	RunnerRegistrationToken string   `json:"runnerRegistrationToken"`
	RunnerName              string   `json:"runnerName"`
	RunnerLabels            []string `json:"runnerLabels"`
}

// DeleteVMRequest structure for POST /delete-vm
type DeleteVMRequest struct {
	VMID string `json:"vmId"`
}

// --- MACHINE IDENTIFIER GENERATION (Refactored from user input) ---

// generateMachineIdentifier generates a random unique ECID and returns it as a base64-encoded
// binary plist, ready for injection into the VM configuration.
func generateMachineIdentifier() (string, error) {
	// 1. Generate a random 64-bit integer
	// The range is [1, 2**63 - 1]. MaxInt64 is 2**63 - 1.
	upperBoundForRandInt := big.NewInt(0).Sub(big.NewInt(math.MaxInt64), big.NewInt(1)) // 2**63 - 2

	randomBigInt, err := crand.Int(crand.Reader, upperBoundForRandInt)
	if err != nil {
		return "", fmt.Errorf("error generating random number: %w", err)
	}
	// Shift range from [0, 2**63 - 3] to [1, 2**63 - 2]
	randomECID := randomBigInt.Uint64() + 1

	// 2. Create the XML plist content with the generated ECID
	plistTemplate := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ECID</key>
	<integer>{{.ECID}}</integer>
</dict>
</plist>`

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return "", fmt.Errorf("error parsing plist template: %w", err)
	}

	var xmlBuffer bytes.Buffer
	err = tmpl.Execute(&xmlBuffer, struct{ ECID uint64 }{ECID: randomECID})
	if err != nil {
		return "", fmt.Errorf("error executing plist template: %w", err)
	}
	plistXMLContent := xmlBuffer.String()

	// 3. Determine plist conversion command based on OS
	var plistCommandName string
	var plistCommandArgs []string

	switch runtime.GOOS {
	case "darwin": // macOS
		plistCommandName = "plutil"
		plistCommandArgs = []string{"-convert", "binary1", "-o", "-", "-"}
	case "linux":
		plistCommandName = "plistutil"
		plistCommandArgs = []string{"-i", "-", "-o", "-", "-f", "bin"}
	default:
		return "", fmt.Errorf("os '%s' not supported for machine ID generation", runtime.GOOS)
	}

	// 4. Convert XML plist to binary plist
	cmd := exec.Command(plistCommandName, plistCommandArgs...)
	cmd.Stdin = bytes.NewReader([]byte(plistXMLContent))

	binaryPlistOutput, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("error converting plist (command: %s): %w. Stderr: %s", plistCommandName, err, exitErr.Stderr)
		}
		return "", fmt.Errorf("error converting plist: %w", err)
	}

	// 5. Base64 encode it
	generatedID := base64.StdEncoding.EncodeToString(binaryPlistOutput)
	return generatedID, nil
}

// --- HELPER FUNCTIONS ---

// checkConfig ensures all necessary environment variables are set.
func checkConfig() error {
	vmRootDir = os.Getenv(VMRootDirEnv)
	imageCacheDir = os.Getenv(ImageCacheEnv)

	if vmRootDir == "" {
		return fmt.Errorf("missing mandatory environment variable: %s", VMRootDirEnv)
	}
	if imageCacheDir == "" {
		return fmt.Errorf("missing mandatory environment variable: %s", ImageCacheEnv)
	}

	// Ensure VM Root Directory exists
	if _, err := os.Stat(vmRootDir); os.IsNotExist(err) {
		log.Printf("Creating VM root directory: %s", vmRootDir)
		if err := os.MkdirAll(vmRootDir, 0755); err != nil {
			return fmt.Errorf("failed to create VM root directory: %w", err)
		}
	}

	log.Printf("Config loaded: VM Root Dir=%s, Image Cache Dir=%s", vmRootDir, imageCacheDir)
	return nil
}

// getActiveVMs scans the VMRootDir for existing VM folders (vm_*).
func getActiveVMs() ([]string, error) {
	mutex.Lock()
	defer mutex.Unlock()

	entries, err := os.ReadDir(vmRootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read VM root directory %s: %w", vmRootDir, err)
	}

	var activeVMs []string
	for _, entry := range entries {
		// Only consider directories starting with "vm_"
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "vm_") {
			activeVMs = append(activeVMs, entry.Name())
		}
	}
	return activeVMs, nil
}

// runCommand executes a shell command and logs its output.
func runCommand(name string, args ...string) error {
	log.Printf("Executing command: %s %v", name, args)
	cmd := exec.Command(name, args...)

	// Create pipes for combined output (stdout and stderr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to set up stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to set up stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("command failed to start: %w", err)
	}

	// Read and log output in real-time
	combinedOutput := io.MultiReader(stdout, stderr)
	go func() {
		// Copy to discard after logging to os.Stdout
		io.Copy(io.Discard, io.TeeReader(combinedOutput, os.Stdout))
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command exited with error: %w", err)
	}
	log.Printf("Command completed successfully: %s", name)
	return nil
}

// --- HANDLERS ---

// handleVMs retrieves the status of active VMs by checking the VMRootDir.
func handleVMs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	activeVMs, err := getActiveVMs()
	if err != nil {
		log.Printf("Error checking active VMs: %v", err)
		http.Error(w, fmt.Sprintf("Error checking active VMs: %v", err), http.StatusInternalServerError)
		return
	}

	vmCount := len(activeVMs)
	availableSlots := MaxActiveVMs - vmCount
	if availableSlots < 0 {
		availableSlots = 0
	}

	status := VMStatus{
		Details:      fmt.Sprintf("VM status based on directory check in %s. Max allowed: %d", vmRootDir, MaxActiveVMs),
		AvailableVMs: availableSlots,
		VMCount:      vmCount,
		VMs:          activeVMs,
		NodeID:       "macvm-node-id", // Placeholder
	}

	json.NewEncoder(w).Encode(status)
}

// handleProvisionVM provisions a new VM only if a slot is available (VMCount < 1).
func handleProvisionVM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Check for capacity limits
	activeVMs, err := getActiveVMs()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error checking capacity: %v", err), http.StatusInternalServerError)
		return
	}

	if len(activeVMs) >= MaxActiveVMs {
		msg := fmt.Sprintf("Node is busy. %d active VM(s) found in %s. Max allowed is %d.", len(activeVMs), vmRootDir, MaxActiveVMs)
		log.Println(msg)
		// Return 429 Too Many Requests to signal the orchestrator to wait.
		http.Error(w, msg, http.StatusTooManyRequests)
		return
	}

	// 2. Decode Request
	var req ProvisionVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.VMID == "" || req.ImageName == "" {
		http.Error(w, "vmId and imageName are required", http.StatusBadRequest)
		return
	}

	// 3. Generate Unique Machine ID
	machineID, err := generateMachineIdentifier()
	if err != nil {
		log.Printf("Failed to generate unique machine identifier: %v", err)
		http.Error(w, fmt.Sprintf("Failed to generate machine ID: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Generated Machine Identifier for %s: %s (Base64)", req.VMID, machineID)

	// 4. Dynamic Path Calculation
	vmPath := filepath.Join(vmRootDir, "vm_"+req.VMID)
	imagePath := filepath.Join(imageCacheDir, req.ImageName, "base.img")

	// 5. Create the VM Directory (This signals "VM existence" for /vms)
	if err := os.MkdirAll(vmPath, 0755); err != nil {
		log.Printf("Failed to create VM directory %s: %v", vmPath, err)
		http.Error(w, fmt.Sprintf("Failed to create VM directory: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Provisioning VM %s using image %s located at %s", req.VMID, req.ImageName, imagePath)

	// 6. Execute macosvm create/run commands
	go func() {
		defer mutex.Unlock()
		mutex.Lock()

		logFile := filepath.Join(vmPath, "vm.log")

		// 6a. macosvm create
		// NOTE: Please confirm the correct flag for injecting the base64-encoded machine ID.
		// We are using -machine-identifier as a placeholder.
		createArgs := []string{
			"create",
			"-name", req.VMID,
			"-path", vmPath,
			"-disk", imagePath,
			"-machine-identifier", machineID, // <-- UNIQUE ID INJECTION
		}

		if err := runCommand("macosvm", createArgs...); err != nil {
			log.Printf("VM %s macosvm create failed: %v", req.VMID, err)
			return
		}

		// 6b. macosvm run
		runArgs := []string{"run", "-name", req.VMID, "-path", vmPath, "-logfile", logFile}
		if err := runCommand("macosvm", runArgs...); err != nil {
			log.Printf("VM %s macosvm run failed: %v", req.VMID, err)
		}
	}()

	w.WriteHeader(http.StatusAccepted) // 202 Accepted: Provisioning has begun
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("VM provisioning initiated for %s. Unique machine ID generated.", req.VMID),
		"vmId":    req.VMID,
		"vmPath":  vmPath,
	})
}

// handleDeleteVM deletes a VM and its associated files.
func handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req DeleteVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.VMID == "" {
		http.Error(w, "vmId is required", http.StatusBadRequest)
		return
	}

	vmPath := filepath.Join(vmRootDir, "vm_"+req.VMID)

	log.Printf("Attempting to delete VM %s at path %s", req.VMID, vmPath)

	// Delete the VM directory and all contents
	go func() {
		defer mutex.Unlock()
		mutex.Lock()

		if err := os.RemoveAll(vmPath); err != nil {
			log.Printf("CRITICAL: Failed to delete VM directory %s: %v", vmPath, err)
		} else {
			log.Printf("Successfully deleted VM directory: %s", vmPath)
		}
	}()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("VM deletion for %s initiated (directory removal).", req.VMID),
		"vmId":    req.VMID,
	})
}

// --- MAIN FUNCTION ---

func main() {
	if err := checkConfig(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	r := mux.NewRouter()

	r.HandleFunc("/vms", handleVMs).Methods("GET")
	r.HandleFunc("/provision-vm", handleProvisionVM).Methods("POST")
	r.HandleFunc("/delete-vm", handleDeleteVM).Methods("POST")

	port := "8081"
	log.Printf("MacVM Agent (Filesystem-Checked with Unique ID) starting on :%s", port)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not listen on :%s: %v", port, err)
	}
}
