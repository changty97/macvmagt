package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/vm" // Import VM manager
)

// Sender sends heartbeats to the orchestrator.
type Sender struct {
	cfg        *config.Config
	vmManager  *vm.Manager
	httpClient *http.Client // Simple HTTP client for orchestrator communication
}

// NewSender creates a new Heartbeat Sender.
func NewSender(cfg *config.Config, vmm *vm.Manager) (*Sender, error) {
	// Revert to simple HTTP client
	httpClient := &http.Client{
		Timeout: 10 * time.Second, // Timeout for heartbeat sending
	}

	return &Sender{
		cfg:        cfg,
		vmManager:  vmm,
		httpClient: httpClient,
	}, nil
}

// StartSendingHeartbeats periodically sends heartbeats to the orchestrator.
func (s *Sender) StartSendingHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Heartbeat sender stopped.")
			return
		case <-ticker.C:
			s.sendHeartbeat()
		}
	}
}

// sendHeartbeat gathers system info and sends it to the orchestrator.
func (s *Sender) sendHeartbeat() {
	// TODO: Implement actual system metric collection (CPU, memory, disk)
	// For now, use placeholders.
	cpuUsage := 25.5
	memUsage := 8.2
	totalMem := 16.0
	diskUsage := 100.5
	totalDisk := 500.0

	// Get current VM info from the VM manager
	vms := s.vmManager.GetVMs()

	payload := models.HeartbeatPayload{
		NodeID:          s.cfg.NodeID,
		VMCount:         len(vms),
		VMs:             vms,
		CPUUsagePercent: cpuUsage,
		MemoryUsageGB:   memUsage,
		TotalMemoryGB:   totalMem,
		DiskUsageGB:     diskUsage,
		TotalDiskGB:     totalDisk,
		Status:          "healthy",                                 // Dynamic status based on actual checks
		CachedImages:    []string{"macos-sonoma", "macos-ventura"}, // TODO: Get from VM manager's cache
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal heartbeat payload: %v", err)
		return
	}

	req, err := http.NewRequest("POST", s.cfg.OrchestratorURL+"/api/heartbeat", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Failed to create heartbeat request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req) // Use the simple HTTP client
	if err != nil {
		log.Printf("Failed to send heartbeat to orchestrator: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Orchestrator returned non-200 status for heartbeat: %s, body: %s", resp.Status, string(bodyBytes))
	} else {
		log.Printf("Heartbeat sent successfully from node %s.", s.cfg.NodeID)
	}
}
