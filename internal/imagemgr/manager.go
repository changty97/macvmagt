package imagemgr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/changty97/macvmagt/internal/config" // Assuming models are shared or duplicated
	"google.golang.org/api/option"
)

// ImageInfo stores metadata about a cached image.
type ImageInfo struct {
	Name          string    // Image name (e.g., "macos-sonoma-github-runner")
	Path          string    // Full path to the cached file
	LastUsed      time.Time // For LRU eviction
	Size          int64     // Size in bytes
	Checksum      string    // SHA256 checksum for verification
	IsDownloading bool      // Flag to indicate if currently downloading
}

// Manager handles caching, downloading, and evicting VM images.
type Manager struct {
	cfg             *config.Config
	cache           map[string]*ImageInfo // Map image name to ImageInfo
	mu              sync.RWMutex          // Protects cache map
	gcsClient       *storage.Client
	downloadQueue   chan string // Channel for images to download
	activeDownloads sync.Map    // Map[string]context.CancelFunc for active downloads
}

// NewManager creates a new Image Manager.
func NewManager(cfg *config.Config) (*Manager, error) {
	// Initialize GCS client
	ctx := context.Background()
	var opts []option.ClientOption
	if cfg.GCPCredentialsPath != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.GCPCredentialsPath))
	} else {
		// Use default application credentials if path is not provided
		log.Println("GCP_CREDENTIALS_PATH not set, using default application credentials.")
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	im := &Manager{
		cfg:           cfg,
		cache:         make(map[string]*ImageInfo),
		gcsClient:     client,
		downloadQueue: make(chan string, 10), // Buffered channel for download requests
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cfg.ImageCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create image cache directory %s: %w", cfg.ImageCacheDir, err)
	}

	// Load existing cached images on startup
	im.loadExistingImages()

	// Start background download worker
	go im.downloadWorker()

	return im, nil
}

// loadExistingImages scans the cache directory and populates the cache map.
func (m *Manager) loadExistingImages() {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, err := os.ReadDir(m.cfg.ImageCacheDir)
	if err != nil {
		log.Printf("Warning: Could not read image cache directory %s: %v", m.cfg.ImageCacheDir, err)
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filePath := filepath.Join(m.cfg.ImageCacheDir, file.Name())
		info, err := os.Stat(filePath)
		if err != nil {
			log.Printf("Warning: Could not stat file %s: %v", filePath, err)
			continue
		}

		// Assuming filename is the image name for simplicity, or you can parse metadata
		imageName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())) // Remove extension
		checksum, err := calculateFileChecksum(filePath)
		if err != nil {
			log.Printf("Warning: Could not calculate checksum for %s: %v", filePath, err)
			checksum = "" // Indicate unknown checksum
		}

		m.cache[imageName] = &ImageInfo{
			Name:     imageName,
			Path:     filePath,
			LastUsed: info.ModTime(), // Use modification time as initial last used
			Size:     info.Size(),
			Checksum: checksum,
		}
		log.Printf("Loaded cached image: %s (%s)", imageName, filePath)
	}
}

// GetCachedImagePath returns the path to a cached image if available and valid.
// It also updates the LastUsed timestamp.
func (m *Manager) GetCachedImagePath(imageName string) (string, bool) {
	m.mu.RLock()
	info, ok := m.cache[imageName]
	m.mu.RUnlock()

	if ok {
		m.mu.Lock()
		info.LastUsed = time.Now() // Update last used
		m.mu.Unlock()
		return info.Path, true
	}
	return "", false
}

// GetCachedImageNames returns a list of names of all currently cached images.
func (m *Manager) GetCachedImageNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.cache))
	for name := range m.cache {
		names = append(names, name)
	}
	return names
}

// RequestImageDownload adds an image to the download queue if not already present or downloading.
func (m *Manager) RequestImageDownload(imageName string) {
	m.mu.RLock()
	info, exists := m.cache[imageName]
	m.mu.RUnlock()

	if exists && !info.IsDownloading {
		log.Printf("Image %s already cached and not downloading.", imageName)
		return
	}
	if exists && info.IsDownloading {
		log.Printf("Image %s is already downloading.", imageName)
		return
	}

	log.Printf("Requesting download for image: %s", imageName)
	// Add to cache as downloading
	m.mu.Lock()
	m.cache[imageName] = &ImageInfo{
		Name:          imageName,
		IsDownloading: true,
	}
	m.mu.Unlock()

	select {
	case m.downloadQueue <- imageName:
		log.Printf("Image %s added to download queue.", imageName)
	default:
		log.Printf("Download queue full for image %s, will retry.", imageName)
		// Handle queue full: could implement a retry mechanism or larger queue
	}
}

// IsImageDownloading checks if a specific image is currently being downloaded.
func (m *Manager) IsImageDownloading(imageName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.cache[imageName]
	return ok && info.IsDownloading
}

// downloadWorker processes image download requests from the queue.
func (m *Manager) downloadWorker() {
	for imageName := range m.downloadQueue {
		log.Printf("Starting download for image: %s", imageName)
		ctx, cancel := context.WithCancel(context.Background())
		m.activeDownloads.Store(imageName, cancel) // Store cancel function

		err := m.downloadImageFromGCS(ctx, imageName)
		m.activeDownloads.Delete(imageName) // Remove cancel function

		m.mu.Lock()
		info, ok := m.cache[imageName]
		if !ok {
			log.Printf("Error: Image %s disappeared from cache during download.", imageName)
			m.mu.Unlock()
			continue
		}
		info.IsDownloading = false // Mark as no longer downloading
		m.mu.Unlock()

		if err != nil {
			log.Printf("Failed to download image %s: %v", imageName, err)
			// On failure, remove from cache so it can be retried
			m.mu.Lock()
			delete(m.cache, imageName)
			m.mu.Unlock()
		} else {
			log.Printf("Successfully downloaded and cached image: %s", imageName)
			m.evictOldImages() // Evict if needed after a successful download
		}
	}
}

// downloadImageFromGCS downloads an image from GCP Cloud Storage.
// Assumes blob name in GCS is the same as imageName (e.g., "macos-sonoma.dmg").
func (m *Manager) downloadImageFromGCS(ctx context.Context, imageName string) error {
	bucket := m.gcsClient.Bucket(m.cfg.GCSBucketName)
	obj := bucket.Object(imageName) // Assuming image name is the object name in GCS

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create GCS object reader for %s: %w", imageName, err)
	}
	defer reader.Close()

	destPath := filepath.Join(m.cfg.ImageCacheDir, imageName)
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", destPath, err)
	}
	defer file.Close()

	hash := sha256.New()
	mw := io.MultiWriter(file, hash)

	bytesCopied, err := io.Copy(mw, reader)
	if err != nil {
		os.Remove(destPath) // Clean up partial download
		return fmt.Errorf("failed to copy data to %s: %w", destPath, err)
	}

	// Get expected checksum from GCS metadata if available, or from a local registry
	// For simplicity, we'll assume the orchestrator or a separate registry provides this.
	// For now, we'll just calculate and store it.
	calculatedChecksum := hex.EncodeToString(hash.Sum(nil))
	log.Printf("Downloaded %s, size: %d bytes, checksum: %s", imageName, bytesCopied, calculatedChecksum)

	// Update cache entry with full details
	m.mu.Lock()
	m.cache[imageName] = &ImageInfo{
		Name:          imageName,
		Path:          destPath,
		LastUsed:      time.Now(),
		Size:          bytesCopied,
		Checksum:      calculatedChecksum,
		IsDownloading: false,
	}
	m.mu.Unlock()

	return nil
}

// evictOldImages implements LRU eviction.
func (m *Manager) evictOldImages() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.cache) <= m.cfg.MaxCachedImages {
		return // No need to evict
	}

	log.Printf("Cache size (%d) exceeds max (%d). Evicting old images...", len(m.cache), m.cfg.MaxCachedImages)

	// Convert map to slice for sorting
	var images []*ImageInfo
	for _, info := range m.cache {
		if !info.IsDownloading { // Don't evict images currently being downloaded
			images = append(images, info)
		}
	}

	// Sort by LastUsed (oldest first)
	sort.Slice(images, func(i, j int) bool {
		return images[i].LastUsed.Before(images[j].LastUsed)
	})

	// Evict until we are within the limit
	for len(images) > m.cfg.MaxCachedImages {
		imageToEvict := images[0]
		log.Printf("Evicting image: %s (last used: %s)", imageToEvict.Name, imageToEvict.LastUsed.Format(time.RFC3339))

		if err := os.Remove(imageToEvict.Path); err != nil {
			log.Printf("Error evicting file %s: %v", imageToEvict.Path, err)
			// If we can't remove the file, don't remove it from cache either,
			// it might be in use or permissions issue.
		} else {
			delete(m.cache, imageToEvict.Name)
			images = images[1:] // Remove from the slice
		}
	}
}

// calculateFileChecksum calculates the SHA256 checksum of a file.
func calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate checksum for %s: %w", filePath, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
