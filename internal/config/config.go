package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds all agent-wide configuration settings.
type Config struct {
	NodeID             string        // Unique identifier for this Mac Mini
	OrchestratorURL    string        // URL of the macvmorx orchestrator
	HeartbeatInterval  time.Duration // How often to send heartbeats
	ImageCacheDir      string        // Directory to store cached VM images
	MaxCachedImages    int           // Maximum number of images to keep in cache (LRU)
	GCSBucketName      string        // GCP Cloud Storage bucket name for images
	GCPCredentialsPath string        // Path to GCP service account key JSON file
	// Add other configurations like VM base path, runner post-script path etc.
}

// LoadConfig loads configuration from environment variables or uses default values.
func LoadConfig() *Config {
	cfg := &Config{
		NodeID:             getEnv("MACVMORX_AGENT_NODE_ID", "mac-mini-default"),
		OrchestratorURL:    getEnv("MACVMORX_ORCHESTRATOR_URL", "http://localhost:8080"),
		HeartbeatInterval:  getEnvDuration("MACVMORX_HEARTBEAT_INTERVAL", 15*time.Second), // 15-30s heartbeat
		ImageCacheDir:      getEnv("MACVMORX_IMAGE_CACHE_DIR", "/var/macvmorx/images_cache"),
		MaxCachedImages:    getEnvInt("MACVMORX_MAX_CACHED_IMAGES", 5),
		GCSBucketName:      getEnv("MACVMORX_GCS_BUCKET_NAME", "macvmorx-vm-images"),
		GCPCredentialsPath: getEnv("MACVMORX_GCP_CREDENTIALS_PATH", ""), // Leave empty for default auth
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

// getEnvInt retrieves an integer environment variable or returns a default value.
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
