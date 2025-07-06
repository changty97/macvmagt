package gcs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// Client provides methods for interacting with Google Cloud Storage.
type Client struct {
	bucketName string
	gcsClient  *storage.Client
}

// NewClient creates a new GCS client.
func NewClient(ctx context.Context, bucketName, credentialsPath string) (*Client, error) {
	var opts []option.ClientOption
	if credentialsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsPath))
	}

	gcsClient, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &Client{
		bucketName: bucketName,
		gcsClient:  gcsClient,
	}, nil
}

// DownloadFile downloads a file from GCS to a local path.
func (c *Client) DownloadFile(ctx context.Context, objectName, localPath string) error {
	log.Printf("Downloading gs://%s/%s to %s", c.bucketName, objectName, localPath)

	rc, err := c.gcsClient.Bucket(c.bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create GCS object reader: %w", err)
	}
	defer rc.Close()

	// Ensure the directory exists
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", dir, err)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("failed to copy data to local file: %w", err)
	}

	log.Printf("Successfully downloaded %s", objectName)
	return nil
}
