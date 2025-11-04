package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds all agent-wide configuration settings.
type Config struct {
	NodeID                 string
	OrchestratorURL        string
	AgentPort              string // Port the agent listens on for commands from orchestrator
	HeartbeatInterval      time.Duration
	ImageCacheDir          string
	MaxCachedImages        int
	GCSBucketName          string
	GCPCredentialsPath     string
	GitHubRunnerScriptPath string // Path to the install_github_runner.sh.template
	VMSSHUser              string // SSH user for connecting to VMs
	VMSSHKeyPath           string // Path to SSH private key for connecting to VMs
}

// LoadConfig loads configuration from environment variables or uses default values.
func LoadConfig() *Config {
	cfg := &Config{
		NodeID:                 getEnv("MACVMORX_AGENT_NODE_ID", "mac-mini-default"),
		OrchestratorURL:        getEnv("MACVMORX_ORCHESTRATOR_URL", "http://localhost:8080"),
		AgentPort:              getEnv("MACVMORX_AGENT_PORT", "8081"),
		HeartbeatInterval:      getEnvDuration("MACVMORX_HEARTBEAT_INTERVAL", 15*time.Second),
		ImageCacheDir:          getEnv("MACVMORX_IMAGE_CACHE_DIR", "/var/macvmorx/images_cache"),
		MaxCachedImages:        getEnvInt("MACVMORX_MAX_CACHED_IMAGES", 5),
		GCSBucketName:          getEnv("MACVMORX_GCS_BUCKET_NAME", "macvmorx-vm-images"),
		GCPCredentialsPath:     getEnv("MACVMORX_GCP_CREDENTIALS_PATH", ""),
		GitHubRunnerScriptPath: getEnv("MACVMORX_GITHUB_RUNNER_SCRIPT_PATH", "/opt/macvmorx-agent/scripts/install_github_runner.sh.template"),
		VMSSHUser:              getEnv("MACVMORX_VM_SSH_USER", "runner"),
		VMSSHKeyPath:           getEnv("MACVMORX_VM_SSH_KEY_PATH", "/Users/runner/.ssh/id_rsa"), // Default path for runner user
	}
	log.Printf("Agent Loaded configuration: %+v", cfg)
	return cfg
}

// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvDuration retrieves a duration environment variable or returns a default value.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			log.Printf("Warning: Could not parse duration for %s='%s', using default %v. Error: %v", key, value, defaultValue, err)
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}

// getEnvInt retrieves an int environment variable or returns a default value.
func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("Warning: Could not parse int for %s='%s', using default %d. Error: %v", key, value, defaultValue, err)
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}
