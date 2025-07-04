package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/imagemgr"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/utils"
	"github.com/changty97/macvmagt/internal/vmgr"
)

// Sender is responsible for collecting system info and sending heartbeats.
type Sender struct {
	cfg          *config.Config
	imageManager *imagemgr.Manager
	vmManager    *vmgr.Manager
}

// NewSender creates a new Heartbeat Sender.
func NewSender(cfg *config.Config, im *imagemgr.Manager, vmm *vmgr.Manager) *Sender {
	return &Sender{
		cfg:          cfg,
		imageManager: im,
		vmManager:    vmm,
	}
}

// StartSendingHeartbeats periodically collects data and sends it to the orchestrator.
func (s *Sender) StartSendingHeartbeats() {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.sendHeartbeat()
	}
}

func (s *Sender) sendHeartbeat() {
	cpuUsage, err := utils.GetCPUUsage()
	if err != nil {
		log.Printf("Error getting CPU usage: %v", err)
		cpuUsage = 0.0 // Report 0 or previous value on error
	}

	memUsed, memTotal, err := utils.GetMemoryUsage()
	if err != nil {
		log.Printf("Error getting memory usage: %v", err)
		memUsed = 0.0
		memTotal = 0.0
	}

	diskUsed, diskTotal, err := utils.GetDiskUsage()
	if err != nil {
		log.Printf("Error getting disk usage: %v", err)
		diskUsed = 0.0
		diskTotal = 0.0
	}

	runningVMs, err := utils.GetRunningVMs() // Use vmutils to get detailed VM info
	if err != nil {
		log.Printf("Error getting running VMs: %v", err)
		runningVMs = []models.VMInfo{}
	}
	vmCount := len(runningVMs)

	cachedImages := s.imageManager.GetCachedImageNames()

	payload := models.HeartbeatPayload{
		NodeID:          s.cfg.NodeID,
		VMCount:         vmCount,
		VMs:             runningVMs,
		CPUUsagePercent: cpuUsage,
		MemoryUsageGB:   memUsed,
		TotalMemoryGB:   memTotal,
		DiskUsageGB:     diskUsed,
		TotalDiskGB:     diskTotal,
		Status:          "healthy", // Determine status based on thresholds later
		CachedImages:    cachedImages,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling heartbeat payload: %v", err)
		return
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/heartbeat", s.cfg.OrchestratorURL), "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error sending heartbeat to orchestrator: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Received non-OK response for heartbeat: %s", resp.Status)
	} else {
		log.Printf("Heartbeat sent successfully from NodeID: %s", s.cfg.NodeID)
	}
}
