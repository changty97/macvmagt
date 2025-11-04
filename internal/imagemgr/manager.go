package imagemgr

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/changty97/macvmagt/internal/config" // Assuming models are shared or duplicated
	"google.golang.org/api/option"
)

// ImageManager handles downloading, caching, and managing VM images.
type ImageManager struct {
	cfg           *config.Config
	storageClient *storage.Client
	cacheMutex    sync.Mutex
	// LRU cache for images (map[imageName]lastUsedTime)
	// For a real LRU, you'd use a more sophisticated data structure like a doubly linked list + map.
	// For simplicity, we'll just track last used time and evict oldest when capacity is reached.
	imageAccessTimes map[string]time.Time // image name -> last access time
}

// NewImageManager creates and initializes a new ImageManager.
func NewImageManager(cfg *config.Config) (*ImageManager, error) {
	ctx := context.Background()
	var client *storage.Client
	var err error

	if cfg.GCPCredentialsPath != "" {
		client, err = storage.NewClient(ctx, option.WithCredentialsFile(cfg.GCPCredentialsPath))
	} else {
		client, err = storage.NewClient(ctx) // Uses Application Default Credentials
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GCP storage client: %w", err)
	}

	// Ensure image cache directory exists
	if err := os.MkdirAll(cfg.ImageCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create image cache directory %s: %w", cfg.ImageCacheDir, err)
	}

	return &ImageManager{
		cfg:              cfg,
		storageClient:    client,
		imageAccessTimes: make(map[string]time.Time),
	}, nil
}

// GetImagePath ensures the image is available locally and returns its path.
// It will download the image if not present and manage the cache.
func (im *ImageManager) GetImagePath(imageName string) (string, error) {
	imagePath := filepath.Join(im.cfg.ImageCacheDir, imageName)

	im.cacheMutex.Lock()
	defer im.cacheMutex.Unlock()

	// Check if image already exists locally
	if _, err := os.Stat(imagePath); err == nil {
		log.Printf("Image '%s' already exists in cache at %s. Updating access time.", imageName, imagePath)
		im.imageAccessTimes[imageName] = time.Now()
		return imagePath, nil
	}

	log.Printf("Image '%s' not found in cache. Attempting to download from GCS bucket '%s'.", imageName, im.cfg.GCSBucketName)

	// Perform LRU eviction if cache is full before downloading
	if len(im.imageAccessTimes) >= im.cfg.MaxCachedImages {
		im.evictOldestImage()
	}

	// Download the image
	err := im.downloadImage(imageName, imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to download image '%s': %w", imageName, err)
	}

	im.imageAccessTimes[imageName] = time.Now() // Record access time after successful download
	return imagePath, nil
}

// downloadImage downloads an image from GCS to the specified local path.
func (im *ImageManager) downloadImage(imageName, localPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // 5-minute timeout for download
	defer cancel()

	rc, err := im.storageClient.Bucket(im.cfg.GCSBucketName).Object(imageName).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create reader for GCS object '%s': %w", imageName, err)
	}
	defer rc.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file '%s': %w", localPath, err)
	}
	defer file.Close()

	if _, err := io.Copy(file, rc); err != nil {
		return fmt.Errorf("failed to copy data to local file '%s': %w", localPath, err)
	}

	log.Printf("Successfully downloaded image '%s' to '%s'.", imageName, localPath)
	return nil
}

// evictOldestImage removes the least recently used image from the cache.
func (im *ImageManager) evictOldestImage() {
	if len(im.imageAccessTimes) == 0 {
		return
	}

	var oldestImage string
	var oldestTime time.Time

	first := true
	for img, t := range im.imageAccessTimes {
		if first || t.Before(oldestTime) {
			oldestTime = t
			oldestImage = img
			first = false
		}
	}

	if oldestImage != "" {
		pathToRemove := filepath.Join(im.cfg.ImageCacheDir, oldestImage)
		log.Printf("Evicting oldest image '%s' from cache (%s).", oldestImage, pathToRemove)
		if err := os.Remove(pathToRemove); err != nil {
			log.Printf("Warning: Failed to remove oldest cached image '%s': %v", oldestImage, err)
		} else {
			delete(im.imageAccessTimes, oldestImage)
			log.Printf("Successfully evicted image '%s'.", oldestImage)
		}
	}
}

// GetCachedImages returns a list of image names currently in the cache.
func (im *ImageManager) GetCachedImages() []string {
	im.cacheMutex.Lock()
	defer im.cacheMutex.Unlock()

	var cached []string
	for img := range im.imageAccessTimes {
		cached = append(cached, img)
	}
	return cached
}
