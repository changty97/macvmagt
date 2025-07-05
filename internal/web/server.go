package web

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
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
	Cfg     *config.Config // Add config to access mTLS paths
}

// NewServer creates a new web server instance for the agent.
func NewServer(port string, handler *api.Handlers, cfg *config.Config) *Server {
	return &Server{
		Port:    port,
		Handler: handler,
		Cfg:     cfg,
	}
}

// Start runs the HTTPS server for receiving orchestrator commands.
func (s *Server) Start() {
	router := mux.NewRouter()

	// Agent API routes for orchestrator commands
	router.HandleFunc("/provision-vm", s.Handler.HandleProvisionVM).Methods("POST")
	router.HandleFunc("/delete-vm", s.Handler.HandleDeleteVM).Methods("POST")

	addr := ":" + s.Port
	log.Printf("Agent server starting on %s (mTLS enabled)", addr)

	// --- mTLS Configuration for the Agent Server ---
	caCert, err := ioutil.ReadFile(s.Cfg.CACertPath)
	if err != nil {
		log.Fatalf("Agent: Failed to read CA certificate for server: %v", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		log.Fatalf("Agent: Failed to append CA certificate to pool.")
	}

	serverCert, err := tls.LoadX509KeyPair(s.Cfg.ServerCertPath, s.Cfg.ServerKeyPath)
	if err != nil {
		log.Fatalf("Agent: Failed to load server certificate and key: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    caCertPool,                     // Trust client certificates signed by our CA (orchestrator's client cert)
		ClientAuth:   tls.RequireAndVerifyClientCert, // Require and verify client certificates (mTLS)
		MinVersion:   tls.VersionTLS12,               // Enforce TLS 1.2 or higher
	}
	tlsConfig.BuildNameToCertificate() // Build map for faster lookups

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
		TLSConfig:    tlsConfig, // Apply the mTLS configuration
	}

	// Use ListenAndServeTLS for HTTPS
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Agent: Could not start server: %v", err)
	}
}
