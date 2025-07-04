package agent

import (
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
	"github.com/gorilla/mux"
)

// Agent represents the MacVMOrx agent running on a Mac Mini.
type Agent struct {
	cfg             *config.Config
	heartbeatSender *heartbeat.Sender
	imageManager    *imagemgr.Manager
	vmManager       *vmgr.Manager
}

// NewAgent creates and initializes a new agent instance.
func NewAgent(cfg *config.Config) (*Agent, error) {
	imageManager, err := imagemgr.NewManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize image manager: %w", err)
	}

	vmManager := vmgr.NewManager(cfg, imageManager)
	heartbeatSender := heartbeat.NewSender(cfg, imageManager, vmManager)

	return &Agent{
		cfg:             cfg,
		heartbeatSender: heartbeatSender,
		imageManager:    imageManager,
		vmManager:       vmManager,
	}, nil
}

// Start runs the agent's main loop and API server.
func (a *Agent) Start() {
	log.Printf("Starting MacVMOrx Agent (NodeID: %s)", a.cfg.NodeID)

	// Start sending heartbeats in a goroutine
	go a.heartbeatSender.StartSendingHeartbeats()

	// Start HTTP server for orchestrator commands (e.g., provision/delete VM)
	router := mux.NewRouter()
	router.HandleFunc("/provision-vm", a.handleProvisionVM).Methods("POST")
	router.HandleFunc("/delete-vm", a.handleDeleteVM).Methods("POST")
	// Add other agent-specific API endpoints if needed

	addr := ":8081" // Agent listens on a different port than orchestrator
	log.Printf("Agent command server starting on %s", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not start agent command server: %v", err)
	}
}

// handleProvisionVM handles requests from the orchestrator to provision a VM.
func (a *Agent) handleProvisionVM(w http.ResponseWriter, r *http.Request) {
	var cmd models.VMProvisionCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding provision VM command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Run provisioning in a goroutine to not block the API handler
	go func() {
		if err := a.vmManager.ProvisionVM(cmd); err != nil {
			log.Printf("Failed to provision VM %s: %v", cmd.VMID, err)
			// TODO: Report provisioning failure back to orchestrator
		} else {
			log.Printf("VM %s provisioning initiated successfully.", cmd.VMID)
			// TODO: Report provisioning success back to orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted) // Acknowledge receipt, provisioning happens in background
	json.NewEncoder(w).Encode(map[string]string{"message": "VM provisioning initiated"})
}

// handleDeleteVM handles requests from the orchestrator to delete a VM.
func (a *Agent) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	var cmd models.VMDeleteCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding delete VM command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Run deletion in a goroutine
	go func() {
		if err := a.vmManager.DeleteVM(cmd); err != nil {
			log.Printf("Failed to delete VM %s: %v", cmd.VMID, err)
			// TODO: Report deletion failure back to orchestrator
		} else {
			log.Printf("VM %s deletion initiated successfully.", cmd.VMID)
			// TODO: Report deletion success back to orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted) // Acknowledge receipt, deletion happens in background
	json.NewEncoder(w).Encode(map[string]string{"message": "VM deletion initiated"})
}
