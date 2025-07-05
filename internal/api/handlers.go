package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/models"
	"github.com/changty97/macvmagt/internal/vm"
)

// Handlers struct holds dependencies for API handlers.
type Handlers struct {
	VMManager *vm.Manager
}

// NewHandlers creates a new Handlers instance for the agent.
func NewHandlers(vmm *vm.Manager) *Handlers {
	return &Handlers{
		VMManager: vmm,
	}
}

// HandleProvisionVM receives a command to provision a new VM.
func (h *Handlers) HandleProvisionVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd models.VMProvisionCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding VM provision command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	log.Printf("Received VM provision command for VM ID: %s, Image: %s", cmd.VMID, cmd.ImageName)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // Give VM provisioning time
		defer cancel()

		err := h.VMManager.ProvisionVM(ctx, cmd)
		if err != nil {
			log.Printf("Failed to provision VM %s: %v", cmd.VMID, err)
			// TODO: Report failure back to orchestrator
		} else {
			log.Printf("VM %s provisioning completed successfully.", cmd.VMID)
			// TODO: Report success back to orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"message": "VM provisioning initiated"})
}

// HandleDeleteVM receives a command to delete a VM.
func (h *Handlers) HandleDeleteVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd models.VMDeleteCommand
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		log.Printf("Error decoding VM delete command: %v", err)
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	log.Printf("Received VM delete command for VM ID: %s", cmd.VMID)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // Give VM deletion time
		defer cancel()

		err := h.VMManager.DeleteVM(ctx, cmd)
		if err != nil {
			log.Printf("Failed to delete VM %s: %v", cmd.VMID, err)
			// TODO: Report failure back to orchestrator
		} else {
			log.Printf("VM %s deletion completed successfully.", cmd.VMID)
			// TODO: Report success back to orchestrator
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"message": "VM deletion initiated"})
}
