package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds all agent-wide configuration settings.
type Config struct {
	NodeID             string
	OrchestratorURL    string
	HeartbeatInterval  time.Duration
	ImageCacheDir      string
	VMsDir             string // Added: Directory for tart VM bundles
	MaxCachedImages    int
	GCSBucketName      string
	GCPCredentialsPath string

	// Removed mTLS Configuration for Agent
	AgentListenPort string // Port for agent to listen for orchestrator commands
}

// LoadConfig loads configuration from environment variables or uses default values.
func LoadConfig() *Config {
	cfg := &Config{
		NodeID:             getEnv("MACVMORX_AGENT_NODE_ID", "mac-mini-default"),
		OrchestratorURL:    getEnv("MACVMORX_ORCHESTRATOR_URL", "http://localhost:8080"), // Changed to HTTP
		HeartbeatInterval:  getEnvDuration("MACVMORX_HEARTBEAT_INTERVAL", 15*time.Second),
		ImageCacheDir:      getEnv("MACVMORX_IMAGE_CACHE_DIR", "/var/macvmorx/images_cache"),
		VMsDir:             getEnv("MACVMORX_VMS_DIR", "/var/macvmorx/vms"), // Added default for VMsDir
		MaxCachedImages:    getEnvInt("MACVMORX_MAX_CACHED_IMAGES", 5),
		GCSBucketName:      getEnv("MACVMORX_GCS_BUCKET_NAME", "macvmorx-vm-images"),
		GCPCredentialsPath: getEnv("MACVMORX_GCP_CREDENTIALS_PATH", ""),

		// Removed mTLS Configuration Defaults for Agent
		AgentListenPort: getEnv("MACVMORX_AGENT_LISTEN_PORT", "8081"), // Port for orchestrator commands
	}
	log.Printf("Loaded agent configuration: %+v", cfg)
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
