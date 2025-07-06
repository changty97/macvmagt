package web

import (
	"log"
	"net/http"
	"time"

	"github.com/changty97/macvmagt/internal/api"
	"github.com/changty97/macvmagt/internal/config"
	"github.com/gorilla/mux"
)

// Server represents the web server for the macvmorx-agent.
type Server struct {
	Port    string
	Handler *api.Handlers
	Cfg     *config.Config // Still need config for port
}

// NewServer creates a new web server instance for the agent.
func NewServer(port string, handler *api.Handlers, cfg *config.Config) *Server {
	return &Server{
		Port:    port,
		Handler: handler,
		Cfg:     cfg,
	}
}

// Start runs the HTTP server for receiving orchestrator commands.
func (s *Server) Start() {
	router := mux.NewRouter()

	// Agent API routes for orchestrator commands
	router.HandleFunc("/provision-vm", s.Handler.HandleProvisionVM).Methods("POST")
	router.HandleFunc("/delete-vm", s.Handler.HandleDeleteVM).Methods("POST")

	addr := ":" + s.Port
	log.Printf("Agent server starting on %s (HTTP enabled)", addr)

	// Revert to simple HTTP server
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Use ListenAndServe for HTTP
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Agent: Could not start server: %v", err)
	}
}
