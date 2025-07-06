package main

import (
	"context"
	"log"
	"os"

	"github.com/changty97/macvmagt/internal/api"
	"github.com/changty97/macvmagt/internal/config"
	"github.com/changty97/macvmagt/internal/gcs"
	"github.com/changty97/macvmagt/internal/heartbeat"
	"github.com/changty97/macvmagt/internal/vm"
	"github.com/changty97/macvmagt/internal/web"
	"github.com/spf13/cobra"
)

var cfg *config.Config // Global config variable

func init() {
	// Load configuration early
	cfg = config.LoadConfig()

	// Use config values as defaults for Cobra flags
	rootCmd.PersistentFlags().StringVar(&cfg.NodeID, "node-id", cfg.NodeID, "Unique identifier for this Mac Mini node")
	rootCmd.PersistentFlags().StringVar(&cfg.OrchestratorURL, "orchestrator-url", cfg.OrchestratorURL, "URL of the macvmorx orchestrator")
	rootCmd.PersistentFlags().DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "How often the agent sends heartbeats")
	rootCmd.PersistentFlags().StringVar(&cfg.ImageCacheDir, "image-cache-dir", cfg.ImageCacheDir, "Directory for VM image cache")
	rootCmd.PersistentFlags().StringVar(&cfg.VMsDir, "vms-dir", cfg.VMsDir, "Directory for tart VM bundles") // Added flag for VMsDir
	rootCmd.PersistentFlags().IntVar(&cfg.MaxCachedImages, "max-cached-images", cfg.MaxCachedImages, "Maximum number of VM images to keep in cache")
	rootCmd.PersistentFlags().StringVar(&cfg.GCSBucketName, "gcs-bucket-name", cfg.GCSBucketName, "Name of the GCP Cloud Storage bucket")
	rootCmd.PersistentFlags().StringVar(&cfg.GCPCredentialsPath, "gcp-credentials-path", cfg.GCPCredentialsPath, "Path to GCP service account key JSON file")
	// Removed mTLS flags for agent
	rootCmd.PersistentFlags().StringVar(&cfg.AgentListenPort, "listen-port", cfg.AgentListenPort, "Port for agent to listen for orchestrator commands")
}

var rootCmd = &cobra.Command{
	Use:   "macvmorx-agent",
	Short: "macvmorx-agent is the client component for MacVMOrx orchestration.",
	Long: `The agent runs on individual Mac Mini machines, reporting health,
managing VM images, and provisioning/deleting VMs as instructed by the orchestrator.`,
	Run: func(cmd *cobra.Command, args []string) {
		startAgent()
	},
}

func startAgent() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize GCS client
	gcsClient, err := gcs.NewClient(ctx, cfg.GCSBucketName, cfg.GCPCredentialsPath)
	if err != nil {
		log.Fatalf("Failed to initialize GCS client: %v", err)
	}

	// Initialize VM Manager
	vmManager := vm.NewManager(cfg, gcsClient)

	// Initialize Heartbeat Sender
	heartbeatSender, err := heartbeat.NewSender(cfg, vmManager)
	if err != nil {
		log.Fatalf("Failed to initialize heartbeat sender: %v", err)
	}
	go heartbeatSender.StartSendingHeartbeats(ctx)

	// Initialize API handlers for incoming orchestrator commands
	apiHandlers := api.NewHandlers(vmManager)

	// Initialize and start the agent's web server
	agentServer := web.NewServer(cfg.AgentListenPort, apiHandlers, cfg)
	agentServer.Start() // This will block
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing agent command: %v", err)
		os.Exit(1)
	}
}
