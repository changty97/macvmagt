package main

import (
	"log"
	"os"

	"github.com/changty97/macvmagt/internal/agent"
	"github.com/changty97/macvmagt/internal/config"
	"github.com/spf13/cobra"
)

var cfg *config.Config // Global config variable

func init() {
	// Load configuration early
	cfg = config.LoadConfig()

	// Use config values as defaults for Cobra flags
	rootCmd.PersistentFlags().StringVar(&cfg.NodeID, "node-id", cfg.NodeID, "Unique identifier for this Mac Mini")
	rootCmd.PersistentFlags().StringVar(&cfg.OrchestratorURL, "orchestrator-url", cfg.OrchestratorURL, "URL of the macvmorx orchestrator")
	rootCmd.PersistentFlags().DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "Interval for sending heartbeats to the orchestrator")
	rootCmd.PersistentFlags().StringVar(&cfg.ImageCacheDir, "image-cache-dir", cfg.ImageCacheDir, "Directory to store cached VM images")
	rootCmd.PersistentFlags().IntVar(&cfg.MaxCachedImages, "max-cached-images", cfg.MaxCachedImages, "Maximum number of images to keep in cache (LRU)")
	rootCmd.PersistentFlags().StringVar(&cfg.GCSBucketName, "gcs-bucket-name", cfg.GCSBucketName, "GCP Cloud Storage bucket name for images")
	rootCmd.PersistentFlags().StringVar(&cfg.GCPCredentialsPath, "gcp-credentials-path", cfg.GCPCredentialsPath, "Path to GCP service account key JSON file (optional)")
}

var rootCmd = &cobra.Command{
	Use:   "macvmagt",
	Short: "macvmagt runs on Mac Minis to manage VMs and report status.",
	Long: `The MacVMOrx Agent is responsible for sending heartbeats to the orchestrator,
provisioning and deleting virtual machines, and managing a local cache of VM images.`,
	Run: func(cmd *cobra.Command, args []string) {
		startAgent()
	},
}

func startAgent() {
	agent, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}
	agent.Start()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing command: %v", err)
		os.Exit(1)
	}
}
