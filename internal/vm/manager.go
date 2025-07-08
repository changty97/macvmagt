package vm

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil" // Added for ioutil.ReadFile
	"log"
	"os" // Added for os.MkdirAll
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/gcs"
	"github.com/changty97/macvmagt/internal/models"
)

const (
	// Default script path for GitHub runner installation within the VM.
	// This script will be executed via SSH after the VM starts.
	GitHubRunnerInstallScriptPath = "/opt/macvmorx-agent/scripts/install_github_runner.sh.template"
)

// Manager handles VM operations like create, delete, and image caching.
type Manager struct {
	cfg            *config.Config
	gcsClient      *gcs.Client
	imageDownloads sync.Map   // Stores map[string]*downloadProgress, tracks ongoing downloads
	vmStatus       sync.Map   // Stores map[string]*models.VMInfo, tracks running VMs
	imageCache     *LRUCache  // Manages cached image files
	mu             sync.Mutex // Protects VM operations
}

// downloadProgress tracks the status of an ongoing image download.
type downloadProgress struct {
	done chan struct{}
	err  error
}

// NewManager creates a new VM Manager.
func NewManager(cfg *config.Config, gcsClient *gcs.Client) *Manager {
	return &Manager{
		cfg:        cfg,
		gcsClient:  gcsClient,
		imageCache: NewLRUCache(cfg.MaxCachedImages),
	}
}

// ProvisionVM creates and starts a new macOS VM.
func (m *Manager) ProvisionVM(ctx context.Context, cmd models.VMProvisionCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("Attempting to provision VM %s with image %s", cmd.VMID, cmd.ImageName)

	imageLocalPath := filepath.Join(m.cfg.ImageCacheDir, cmd.ImageName)

	// Check if image is already downloaded or is currently downloading
	if _, ok := m.imageCache.Get(cmd.ImageName); !ok {
		// Image not in cache, initiate download if not already in progress
		progress, loaded := m.imageDownloads.LoadOrStore(cmd.ImageName, &downloadProgress{done: make(chan struct{})})
		dp := progress.(*downloadProgress)

		if !loaded { // If we are the ones initiating the download
			go func() {
				log.Printf("Starting download of image %s from GCS...", cmd.ImageName)
				dp.err = m.gcsClient.DownloadFile(ctx, cmd.ImageName, imageLocalPath)
				if dp.err != nil {
					log.Printf("Failed to download image %s: %v", cmd.ImageName, dp.err)
				} else {
					log.Printf("Image %s downloaded successfully.", cmd.ImageName)
					m.imageCache.Add(cmd.ImageName, imageLocalPath) // Add to LRU cache
				}
				close(dp.done)
				m.imageDownloads.Delete(cmd.ImageName) // Remove from ongoing downloads
			}()
		}

		// Wait for download to complete
		log.Printf("Waiting for image %s download to complete...", cmd.ImageName)
		select {
		case <-dp.done:
			if dp.err != nil {
				return fmt.Errorf("image download failed: %w", dp.err)
			}
			log.Printf("Image %s is ready for VM provisioning.", cmd.ImageName)
		case <-ctx.Done():
			return ctx.Err() // Context cancelled while waiting for download
		}
	} else {
		log.Printf("Image %s already cached, skipping download.", cmd.ImageName)
	}

	// Use m.cfg.VMsDir for VM directory
	vmDir := filepath.Join(m.cfg.VMsDir, cmd.VMID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return fmt.Errorf("failed to create VM directory %s: %w", vmDir, err)
	}

	// 1. Clone the image
	// log.Printf("Cloning image %s to VM %s...", cmd.ImageName, cmd.VMID)
	// cloneCmd := exec.Command("tart", "clone", cmd.ImageName, cmd.VMID)
	// cloneCmd.Dir = m.cfg.ImageCacheDir // tart expects to clone from its working directory
	// if output, err := cloneCmd.CombinedOutput(); err != nil {
	// 	return fmt.Errorf("failed to clone image %s to %s: %w\nOutput: %s", cmd.ImageName, cmd.VMID, err, output)
	// }
	// log.Printf("Image %s cloned to VM %s successfully.", cmd.ImageName, cmd.VMID)

	// 2. Start the VM
	log.Printf("Starting VM %s...", cmd.VMID)
	// tart run --dir /var/macvmorx/vms <VMID> --disk <path to cloned disk> --ephemeral
	// Note: tart run automatically uses the cloned VM's disk within the tart home directory.
	// The --dir flag for tart run specifies the working directory for tart, where it expects to find VMs.
	startCmd := exec.Command("tart", "run", cmd.VMID, "--ephemeral", "--restore-from", cmd.ImageName)
	startCmd.Dir = m.cfg.VMsDir // tart expects to find VM bundles in this directory
	// Use a non-blocking approach for starting the VM, or capture its output for debugging
	// For now, let's just run it and assume it starts.
	// In a real scenario, you'd want to capture stdout/stderr and potentially detach.
	if output, err := startCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start VM %s: %w\nOutput: %s", cmd.VMID, err, output)
	}
	log.Printf("VM %s started successfully.", cmd.VMID)

	// 3. Get VM IP Address (this is highly dependent on your tart network setup)
	// This is a placeholder. You'll need a reliable way to get the VM's IP.
	// E.g., via `tart ip <VMID>` if tart supports it reliably, or DHCP leases, or cloud-init.
	vmIP := "127.0.0.1" // Placeholder
	log.Printf("Assuming VM %s IP is %s (PLACEHOLDER)", cmd.VMID, vmIP)

	// 4. Execute post-provisioning script (e.g., install GitHub Actions runner)
	// This requires SSH access to the VM.
	log.Printf("Executing post-provisioning script on VM %s...", cmd.VMID)
	runnerScriptTemplate, err := ioutil.ReadFile(GitHubRunnerInstallScriptPath)
	if err != nil {
		return fmt.Errorf("failed to read runner install script template: %w", err)
	}

	tmpl, err := template.New("runnerScript").Parse(string(runnerScriptTemplate))
	if err != nil {
		return fmt.Errorf("failed to parse runner script template: %w", err)
	}

	var scriptBuffer bytes.Buffer
	templateData := struct {
		RunnerName              string
		RunnerRegistrationToken string
		RunnerLabels            string // Pass as comma-separated string if needed
	}{
		RunnerName:              cmd.RunnerName,
		RunnerRegistrationToken: cmd.RunnerRegistrationToken,
		RunnerLabels:            strings.Join(cmd.RunnerLabels, ","),
	}
	if err := tmpl.Execute(&scriptBuffer, templateData); err != nil {
		return fmt.Errorf("failed to execute runner script template: %w", err)
	}

	// SSH into the VM and run the script
	// This is a simplified SSH command. In production, use a dedicated SSH client library
	// (e.g., golang.org/x/crypto/ssh) for better control, error handling, and security.
	sshCmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"runner@"+vmIP, "bash -s") // Assuming 'runner' user in VM
	sshCmd.Stdin = &scriptBuffer
	if output, err := sshCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to execute runner install script on VM %s: %w\nOutput: %s", cmd.VMID, err, output)
	}
	log.Printf("Post-provisioning script executed successfully on VM %s.", cmd.VMID)

	// Update VM status in memory
	m.vmStatus.Store(cmd.VMID, &models.VMInfo{
		VMID:           cmd.VMID,
		ImageName:      cmd.ImageName,
		VMHostname:     cmd.RunnerName, // Use runner name as VM hostname for now
		VMIPAddress:    vmIP,
		RuntimeSeconds: 0, // Will be updated by heartbeats
	})

	log.Printf("VM %s provisioned and runner configured.", cmd.VMID)
	return nil
}

// DeleteVM stops and deletes a VM.
func (m *Manager) DeleteVM(ctx context.Context, cmd models.VMDeleteCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("Attempting to delete VM %s...", cmd.VMID)

	// 1. Stop the VM (if running)
	stopCmd := exec.Command("tart", "stop", cmd.VMID)
	if output, err := stopCmd.CombinedOutput(); err != nil {
		// Log error but continue, as it might already be stopped
		log.Printf("Warning: Failed to stop VM %s (might not be running): %v\nOutput: %s", cmd.VMID, err, output)
	} else {
		log.Printf("VM %s stopped.", cmd.VMID)
	}

	// 2. Delete the VM
	deleteCmd := exec.Command("tart", "delete", cmd.VMID)
	// Use m.cfg.VMsDir for tart delete if it's expected to be run from that directory
	// Assuming tart delete can find the VM by ID regardless of CWD if it's in its default location.
	// If tart requires CWD to be the parent of the VM bundle, uncomment the line below.
	// deleteCmd.Dir = m.cfg.VMsDir
	if output, err := deleteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete VM %s: %w\nOutput: %s", cmd.VMID, err, output)
	}
	log.Printf("VM %s deleted successfully.", cmd.VMID)

	m.vmStatus.Delete(cmd.VMID) // Remove from tracking

	return nil
}

// GetVMs returns a list of currently tracked VMs.
func (m *Manager) GetVMs() []models.VMInfo {
	var vms []models.VMInfo
	m.vmStatus.Range(func(key, value interface{}) bool {
		vmInfo := value.(*models.VMInfo)
		// Update runtime seconds for display (simple approximation)
		// In a real system, you'd track start time and calculate more accurately.
		// Corrected: Cast float64 to int64
		vmInfo.RuntimeSeconds = int64(time.Since(time.Now().Add(-time.Duration(vmInfo.RuntimeSeconds) * time.Second)).Seconds())
		vms = append(vms, *vmInfo)
		return true
	})
	return vms
}

// LRUCache implements a simple LRU cache for VM images.
type LRUCache struct {
	capacity int
	queue    []string          // Stores image names, LRU at head, MRU at tail
	items    map[string]string // Stores imageName -> localPath
	mu       sync.Mutex
}

// NewLRUCache creates a new LRU cache with the given capacity.
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		queue:    make([]string, 0, capacity),
		items:    make(map[string]string),
	}
}

// Add adds an item to the cache. If capacity is exceeded, it evicts the LRU item.
func (c *LRUCache) Add(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.items[key]; ok {
		// Item already exists, move to MRU
		c.moveToMRU(key)
		return
	}

	if len(c.queue) >= c.capacity {
		// Evict LRU item
		lruKey := c.queue[0]
		delete(c.items, lruKey)
		c.queue = c.queue[1:]
		log.Printf("Evicted LRU image: %s", lruKey)
		// TODO: Delete the actual file from disk here
	}

	c.queue = append(c.queue, key)
	c.items[key] = value
	log.Printf("Added image to cache: %s", key)
}

// Get retrieves an item from the cache and marks it as recently used.
func (c *LRUCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.items[key]
	if ok {
		c.moveToMRU(key)
	}
	return value, ok
}

// moveToMRU moves an item to the most recently used position in the queue.
func (c *LRUCache) moveToMRU(key string) {
	for i, k := range c.queue {
		if k == key {
			c.queue = append(c.queue[:i], c.queue[i+1:]...) // Remove
			c.queue = append(c.queue, key)                  // Add to end
			break
		}
	}
}
