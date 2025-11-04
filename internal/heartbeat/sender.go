package heartbeat

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/imagemgr"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/utils"
	"github.com/changty97/macvmagt/internal/vmgr" // Import VM manager
)

// Sender handles sending periodic heartbeats to the orchestrator.
type Sender struct {
	cfg          *config.Config
	vmManager    *vmgr.VMManager
	imageManager *imagemgr.ImageManager
	httpClient   *http.Client
}

// NewSender creates a new Heartbeat Sender.
func NewSender(cfg *config.Config, vmm *vmgr.VMManager, imm *imagemgr.ImageManager) *Sender {
	return &Sender{
		cfg:          cfg,
		vmManager:    vmm,
		imageManager: imm,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
}

// StartSendingHeartbeats starts a goroutine that periodically sends heartbeats.
func (s *Sender) StartSendingHeartbeats() {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.SendHeartbeat()
	}
}

// SendHeartbeat collects current node status and sends it to the orchestrator.
func (s *Sender) SendHeartbeat() {
	cpuUsage := utils.GetCPUUsage()
	memUsed, memTotal := utils.GetMemoryUsage()
	diskUsed, diskTotal := utils.GetDiskUsage()
	runningVMs := s.vmManager.GetRunningVMs()
	cachedImages := s.imageManager.GetCachedImages()

	// Update runtime for running VMs before sending heartbeat
	for i := range runningVMs {
		// This is a simplified update. In a real system, you'd need to query
		// tart for actual VM uptime or track it from VM start time.
		// For now, we'll just increment a dummy counter or rely on tart's reporting.
		// For this example, we'll just assume the VMInfo in vmManager is kept up-to-date
		// by parsing `tart list` output periodically or on VM start.
		// For the purpose of this demo, we'll leave it as 0 or a fixed value.
		// A real implementation would involve polling tart or tracking start time.
		// Let's make a simple assumption for demonstration:
		if runningVMs[i].VMStartTime != nil {
			runningVMs[i].RuntimeSeconds = int64(time.Since(*runningVMs[i].VMStartTime).Seconds())
		}
	}

	payload := models.HeartbeatPayload{
		NodeID:          s.cfg.NodeID,
		VMCount:         len(runningVMs),
		VMs:             runningVMs,
		CPUUsagePercent: cpuUsage,
		MemoryUsageGB:   memUsed,
		TotalMemoryGB:   memTotal,
		DiskUsageGB:     diskUsed,
		TotalDiskGB:     diskTotal,
		Status:          "healthy", // Agent's self-reported status
		CachedImages:    cachedImages,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling heartbeat payload: %v", err)
		return
	}

	resp, err := s.httpClient.Post(s.cfg.OrchestratorURL+"/api/heartbeat", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error sending heartbeat to orchestrator %s: %v", s.cfg.OrchestratorURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Orchestrator returned non-200 status for heartbeat: %s, body: %s", resp.Status, string(bodyBytes))
	} else {
		log.Printf("Heartbeat sent successfully to orchestrator %s", s.cfg.OrchestratorURL)
	}
}
