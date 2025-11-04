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

	MaxActiveVMs = 2 // Enforce the limit of only one active VM per Mac Mini
)

var (
	vmRootDir     string
	imageCacheDir string
	mutex         sync.Mutex // Mutex to protect concurrent access to the VM directory check/creation
	nodeHostname  string     // Global variable to store the system's hostname/domain ID
)

// --- CONFIGURATION STRUCTS (Replacing VM_CONFIG_JSON_TEMPLATE) ---

// StorageDevice mirrors the objects in the "storage" array in config.json
type StorageDevice struct {
	Type     string `json:"type"`
	File     string `json:"file"` // This will now hold the ABSOLUTE path
	ReadOnly bool   `json:"readOnly"`
}

// DisplayConfig mirrors the objects in the "displays" array in config.json
type DisplayConfig struct {
	DPI    int `json:"dpi"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// NetworkConfig mirrors the objects in the "networks" array in config.json
type NetworkConfig struct {
	Type string `json:"type"`
}

// VMConfig is the primary structure for the VM's config.json
type VMConfig struct {
	HardwareModel string          `json:"hardwareModel"`
	Storage       []StorageDevice `json:"storage"`
	RAM           uint64          `json:"ram"`
	MachineID     string          `json:"machineId"`
	Displays      []DisplayConfig `json:"displays"`
	Version       int             `json:"version"`
	CPUs          int             `json:"cpus"`
	Networks      []NetworkConfig `json:"networks"`
	Audio         bool            `json:"audio"`
}

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

// vmPaths aggregates all relevant file paths for a VM.
type vmPaths struct {
	vmRootPath          string
	imageName           string
	vmDiskDestPath      string
	vmAuxDestPath       string
	vmConfigPath        string
	vmLogPath           string
	vmPIDPath           string
	imageDiskSourcePath string
	imageAuxSourcePath  string
}

// --- MACHINE IDENTIFIER GENERATION ---

// generateMachineIdentifier generates a random unique ECID (Base64-encoded binary plist).
func generateMachineIdentifier() (string, error) {
	// 1. Generate a random 64-bit integer
	upperBoundForRandInt := big.NewInt(0).Sub(big.NewInt(math.MaxInt64), big.NewInt(1))
	randomBigInt, err := crand.Int(crand.Reader, upperBoundForRandInt)
	if err != nil {
		return "", fmt.Errorf("error generating random number: %w", err)
	}
	randomECID := randomBigInt.Uint64() + 1

	// 2. Create the XML plist content
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

// getVMPaths calculates all necessary file paths for a given VM ID and Image Name.
func getVMPaths(vmID, imageName string) vmPaths {
	vmPath := filepath.Join(vmRootDir, "vm_"+vmID)

	// Default paths for source images.
	imageDiskSourcePath := ""
	imageAuxSourcePath := ""
	if imageName != "" {
		imageDiskSourcePath = filepath.Join(imageCacheDir, imageName, "base.img")
		imageAuxSourcePath = filepath.Join(imageCacheDir, imageName, "aux.img")
	}

	return vmPaths{
		vmRootPath: vmPath,
		imageName:  imageName,
		// Full, absolute paths for disk files (requested change)
		vmDiskDestPath:      filepath.Join(vmPath, "disk.img"),
		vmAuxDestPath:       filepath.Join(vmPath, "aux.img"),
		vmConfigPath:        filepath.Join(vmPath, "config.json"),
		vmLogPath:           filepath.Join(vmPath, "vm.log"),
		vmPIDPath:           filepath.Join(vmPath, "vm.pid"),
		imageDiskSourcePath: imageDiskSourcePath,
		imageAuxSourcePath:  imageAuxSourcePath,
	}
}

// checkConfig ensures all necessary environment variables are set and retrieves the system hostname.
func checkConfig() error {
	var err error

	// Get the system's hostname for the NodeID
	nodeHostname, err = os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get system hostname (Node ID): %w", err)
	}

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

	log.Printf("Config loaded: VM Root Dir=%s, Image Cache Dir=%s, Node ID=%s", vmRootDir, imageCacheDir, nodeHostname)
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

// copyFile copies a file from src to dst.
func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, fmt.Errorf("source file stat error: %w", err)
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("source is not a regular file: %s", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("error opening source file: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("error creating destination file: %w", err)
	}
	defer destination.Close()

	nBytes, err := io.Copy(destination, source)
	if err != nil {
		return 0, fmt.Errorf("error copying file: %w", err)
	}

	return nBytes, nil
}

// startVMInBackground launches the VM process non-blocking and records its PID and output.
func startVMInBackground(vmID string, paths vmPaths) error {
	logFile, err := os.Create(paths.vmLogPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	log.Printf("Executing command: macosvm -g %s", paths.vmConfigPath)

	cmd := exec.Command("macosvm", "-g", paths.vmConfigPath)

	// Redirect Stdout and Stderr to the log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close() // Close file if start fails immediately
		return fmt.Errorf("failed to start macosvm: %w", err)
	}

	// 1. Capture and write the PID
	pid := cmd.Process.Pid
	if err := os.WriteFile(paths.vmPIDPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		log.Printf("CRITICAL: Failed to write PID file for %s: %v. VM may be running without trackable PID.", vmID, err)
	} else {
		log.Printf("VM %s started in background with PID %d. PID file: %s", vmID, pid, paths.vmPIDPath)
	}

	// 2. Monitor the process exit in a non-blocking goroutine
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("VM %s (PID %d) process finished with error: %v. VM files still exist.", vmID, pid, err)
		} else {
			log.Printf("VM %s (PID %d) process exited normally. VM files still exist.", vmID, pid)
		}
		logFile.Close()
	}()

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
		NodeID:       nodeHostname, // Use the dynamically retrieved hostname
	}

	json.NewEncoder(w).Encode(status)
}

// handleProvisionVM provisions a new VM by setting up files and running the process in the background.
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

	// 3. Generate Unique Machine ID (ECID)
	machineID, err := generateMachineIdentifier()
	if err != nil {
		log.Printf("Failed to generate unique machine identifier: %v", err)
		http.Error(w, fmt.Sprintf("Failed to generate machine ID: %v", err), http.StatusInternalServerError)
		return
	}

	// 4. Calculate Paths
	paths := getVMPaths(req.VMID, req.ImageName)

	// 5. Create the VM Directory
	if err := os.MkdirAll(paths.vmRootPath, 0755); err != nil {
		log.Printf("Failed to create VM directory %s: %v", paths.vmRootPath, err)
		http.Error(w, fmt.Sprintf("Failed to create VM directory: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Provisioning VM %s in %s", req.VMID, paths.vmRootPath)

	// Provisioning steps (must be synchronous)
	if _, err := copyFile(paths.imageDiskSourcePath, paths.vmDiskDestPath); err != nil {
		log.Printf("VM %s DISK copy failed: %v. Cleaning up directory.", req.VMID, err)
		os.RemoveAll(paths.vmRootPath)
		http.Error(w, fmt.Sprintf("DISK copy failed: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Copied %s to %s", paths.imageDiskSourcePath, paths.vmDiskDestPath)

	if _, err := copyFile(paths.imageAuxSourcePath, paths.vmAuxDestPath); err != nil {
		log.Printf("VM %s AUX copy failed: %v. Cleaning up directory.", req.VMID, err)
		os.RemoveAll(paths.vmRootPath)
		http.Error(w, fmt.Sprintf("AUX copy failed: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Copied %s to %s", paths.imageAuxSourcePath, paths.vmAuxDestPath)

	// 5. Create Configuration Content using Go struct (addressing user request 1 & 2)
	config := VMConfig{
		// Hardcoded value for hardwareModel needed by macosvm
		HardwareModel: "YnBsaXN0MDDRAQJURUNYWFZlWlhvWEtTWFpZcmZlWmdYSlh4Y3l5Y29YSlh4Y3l5Y29YSlh4Y3l5Y29XRFdEUjIwVFlfU0hPcnJvdHRlckFycGltZQAAAAABt",
		Storage: []StorageDevice{
			{Type: "disk", File: paths.vmDiskDestPath, ReadOnly: false}, // **Absolute Path**
			{Type: "aux", File: paths.vmAuxDestPath, ReadOnly: false},   // **Absolute Path**
		},
		RAM:       4294967296, // 4GB
		MachineID: machineID,
		Displays: []DisplayConfig{
			{DPI: 200, Width: 2560, Height: 1600},
		},
		Version: 1,
		CPUs:    4,
		Networks: []NetworkConfig{
			{Type: "nat"},
		},
		Audio: false,
	}

	// Marshal the struct to pretty-printed JSON
	configContent, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("VM %s failed to marshal config struct: %v. Cleaning up directory.", req.VMID, err)
		os.RemoveAll(paths.vmRootPath)
		http.Error(w, fmt.Sprintf("Failed to generate config JSON: %v", err), http.StatusInternalServerError)
		return
	}

	// Write config.json
	if err := os.WriteFile(paths.vmConfigPath, configContent, 0644); err != nil {
		log.Printf("VM %s failed to write config.json: %v. Cleaning up directory.", req.VMID, err)
		os.RemoveAll(paths.vmRootPath)
		http.Error(w, fmt.Sprintf("Failed to write config.json: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Wrote VM configuration file: %s", paths.vmConfigPath)

	// 6. Start the VM process in the background
	if err := startVMInBackground(req.VMID, paths); err != nil {
		log.Printf("VM %s failed to start background process: %v. Cleaning up directory.", req.VMID, err)
		os.RemoveAll(paths.vmRootPath)
		http.Error(w, fmt.Sprintf("Failed to start VM: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted) // 202 Accepted: Provisioning has begun
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("VM provisioning and start initiated for %s. Process running in background.", req.VMID),
		"vmId":    req.VMID,
		"vmPath":  paths.vmRootPath,
	})
}

// handleDeleteVM deletes a VM and its associated files, including terminating the process.
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

	// Get Paths (image name is not required for deletion)
	paths := getVMPaths(req.VMID, "")

	log.Printf("Attempting to delete VM %s at path %s", req.VMID, paths.vmRootPath)

	// Delete the VM directory and all contents in a background goroutine
	go func() {
		defer mutex.Unlock()
		mutex.Lock()

		// 1. Try to kill the running process using the recorded PID
		pidBytes, err := os.ReadFile(paths.vmPIDPath)
		if err == nil {
			pid := strings.TrimSpace(string(pidBytes))
			log.Printf("Found PID %s for VM %s. Attempting to kill process...", pid, req.VMID)

			// Execute kill -9 <PID>
			killCmd := exec.Command("kill", "-9", pid)
			if killErr := killCmd.Run(); killErr != nil {
				// The kill command fails if the process is already gone, which is expected and acceptable.
				log.Printf("VM %s: Kill command failed (process likely already stopped): %v", req.VMID, killErr)
			} else {
				log.Printf("VM %s: Successfully sent KILL signal to PID %s.", req.VMID, pid)
			}
		} else if !os.IsNotExist(err) {
			log.Printf("VM %s: Warning: Could not read PID file %s: %v. Proceeding with directory removal.", req.VMID, paths.vmPIDPath, err)
		} else {
			log.Printf("VM %s: PID file %s not found. Process likely stopped already. Proceeding with directory removal.", req.VMID, paths.vmPIDPath)
		}

		// 2. Delete the VM directory and all contents
		if err := os.RemoveAll(paths.vmRootPath); err != nil {
			log.Printf("CRITICAL: Failed to delete VM directory %s: %v", paths.vmRootPath, err)
		} else {
			log.Printf("Successfully deleted VM directory: %s", paths.vmRootPath)
		}
	}()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("VM deletion for %s initiated (process termination and directory removal).", req.VMID),
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
	log.Printf("MacVM Agent (Manual Provisioning and NodeID) starting on :%s", port)

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
