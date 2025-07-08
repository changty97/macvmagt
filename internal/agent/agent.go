package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/heartbeat"
	"github.com/changty97/macvmagt/internal/imagemgr"
	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/vmgr"
)

// Agent is the main struct for the macvmorx agent.
type Agent struct {
	cfg          *config.Config
	vmManager    *vmgr.VMManager
	imageManager *imagemgr.ImageManager
	hbSender     *heartbeat.Sender
}

// NewAgent creates and initializes a new Agent instance.
func NewAgent(cfg *config.Config) (*Agent, error) {
	imageManager, err := imagemgr.NewImageManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create image manager: %w", err)
	}

	vmManager, err := vmgr.NewVMManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM manager: %w", err)
	}

	hbSender := heartbeat.NewSender(cfg, vmManager, imageManager)

	return &Agent{
		cfg:          cfg,
		vmManager:    vmManager,
		imageManager: imageManager,
		hbSender:     hbSender,
	}, nil
}

// StartAgent starts the agent's services.
func (a *Agent) StartAgent() {
	// Start heartbeat sender in a goroutine
	go a.hbSender.StartSendingHeartbeats()

	// Set up HTTP server for receiving commands from the orchestrator
	http.HandleFunc("/provision-vm", a.handleProvisionVM)
	http.HandleFunc("/delete-vm", a.handleDeleteVM)

	addr := ":" + a.cfg.AgentPort
	log.Printf("Agent listening for orchestrator commands on %s (HTTP enabled)", addr)

	srv := &http.Server{
		Addr:         addr,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Agent server could not start: %v", err)
	}
}

// handleProvisionVM handles requests from the orchestrator to provision a VM.
func (a *Agent) handleProvisionVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd models.VMProvisionCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding provision VM command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	log.Printf("Received provision VM command for VMID: %s, Image: %s", cmd.VMID, cmd.ImageName)

	// Get image path (downloads if not cached)
	imagePath, err := a.imageManager.GetImagePath(cmd.ImageName)
	if err != nil {
		log.Printf("Failed to get image path for '%s': %v", cmd.ImageName, err)
		http.Error(w, fmt.Sprintf("Failed to prepare VM image: %v", err), http.StatusInternalServerError)
		return
	}

	// Provision the VM in a goroutine to not block the HTTP response
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // Allow ample time for VM provisioning
		defer cancel()

		vmInfo, provisionErr := a.vmManager.ProvisionVM(ctx, cmd, imagePath)
		if provisionErr != nil {
			log.Printf("Error provisioning VM '%s': %v", cmd.VMID, provisionErr)
			// In a real system, you'd send a failure status back to the orchestrator
		} else {
			log.Printf("VM '%s' provisioned successfully. IP: %s", vmInfo.VMID, vmInfo.VMIPAddress)
			// In a real system, you'd send a success status back to the orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted) // Acknowledge the command, provisioning happens asynchronously
	json.NewEncoder(w).Encode(map[string]string{"message": "VM provisioning initiated"})
}

// handleDeleteVM handles requests from the orchestrator to delete a VM.
func (a *Agent) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd models.VMDeleteCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding delete VM command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	log.Printf("Received delete VM command for VMID: %s", cmd.VMID)

	// Delete the VM in a goroutine to not block the HTTP response
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // Allow time for VM deletion
		defer cancel()

		deleteErr := a.vmManager.DeleteVM(ctx, cmd)
		if deleteErr != nil {
			log.Printf("Error deleting VM '%s': %v", cmd.VMID, deleteErr)
			// In a real system, you'd send a failure status back to the orchestrator
		} else {
			log.Printf("VM '%s' deleted successfully.", cmd.VMID)
			// In a real system, you'd send a success status back to the orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted) // Acknowledge the command, deletion happens asynchronously
	json.NewEncoder(w).Encode(map[string]string{"message": "VM deletion initiated"})
}
