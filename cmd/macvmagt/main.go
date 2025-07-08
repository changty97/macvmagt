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
	rootCmd.PersistentFlags().StringVar(&cfg.NodeID, "node-id", cfg.NodeID, "Unique identifier for this Mac Mini agent")
	rootCmd.PersistentFlags().StringVar(&cfg.OrchestratorURL, "orchestrator-url", cfg.OrchestratorURL, "URL of the macvmorx orchestrator")
	rootCmd.PersistentFlags().StringVar(&cfg.AgentPort, "agent-port", cfg.AgentPort, "Port the agent listens on for orchestrator commands")
	rootCmd.PersistentFlags().DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "How often the agent sends heartbeats")
	rootCmd.PersistentFlags().StringVar(&cfg.ImageCacheDir, "image-cache-dir", cfg.ImageCacheDir, "Directory for VM image cache")
	rootCmd.PersistentFlags().IntVar(&cfg.MaxCachedImages, "max-cached-images", cfg.MaxCachedImages, "Maximum number of VM images to keep in cache")
	rootCmd.PersistentFlags().StringVar(&cfg.GCSBucketName, "gcs-bucket-name", cfg.GCSBucketName, "Name of the GCP Cloud Storage bucket for VM images")
	rootCmd.PersistentFlags().StringVar(&cfg.GCPCredentialsPath, "gcp-credentials-path", cfg.GCPCredentialsPath, "Path to GCP service account key JSON file")
	rootCmd.PersistentFlags().StringVar(&cfg.GitHubRunnerScriptPath, "github-runner-script-path", cfg.GitHubRunnerScriptPath, "Path to the GitHub runner installation script template")
	rootCmd.PersistentFlags().StringVar(&cfg.VMSSHUser, "vm-ssh-user", cfg.VMSSHUser, "SSH username for connecting to VMs")
	rootCmd.PersistentFlags().StringVar(&cfg.VMSSHKeyPath, "vm-ssh-key-path", cfg.VMSSHKeyPath, "Path to SSH private key for connecting to VMs")
}

var rootCmd = &cobra.Command{
	Use:   "macvmorx-agent",
	Short: "macvmorx-agent is the client-side component for macOS VM orchestration.",
	Long: `The macvmorx-agent runs on individual Mac Mini machines to manage VMs,
report node health, and handle image caching as instructed by the orchestrator.`,
	Run: func(cmd *cobra.Command, args []string) {
		startAgent()
	},
}

func startAgent() {
	// Configure logging to file if LogFilePath is provided (from orchestrator config, agent doesn't have this directly)
	// For agent, logs will go to stdout/stderr by default, or be redirected by launchd.
	// If you want a log file for the agent, you'd add a similar config option here.
	// For now, it logs to console, which launchd can redirect.

	log.Printf("Starting macvmorx-agent with NodeID: %s", cfg.NodeID)

	agent, err := agent.NewAgent(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	agent.StartAgent()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error executing command: %v", err)
		os.Exit(1)
	}
}
