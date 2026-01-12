// Package reader provides functionality to read blobs from Azure Storage Account.
package reader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// SortOrder defines the order for listing blobs.
type SortOrder string

const (
	// SortOldestFirst sorts blobs by last modified time, oldest first.
	SortOldestFirst SortOrder = "oldest"
	// SortNewestFirst sorts blobs by last modified time, newest first.
	SortNewestFirst SortOrder = "newest"
)

// BlobInfo contains metadata about a blob.
type BlobInfo struct {
	Name         string
	Size         int64
	ContentType  string
	LastModified time.Time
}

// Reader provides methods to interact with Azure Blob Storage.
type Reader struct {
	client             *azblob.Client
	storageAccountName string
	containerName      string
	pattern            *regexp.Regexp
	logger             *slog.Logger
}

// Config holds the configuration for the blob reader.
type Config struct {
	StorageAccountName string
	ContainerName      string
	FilePattern        string
}

// New creates a new Reader instance using DefaultAzureCredential.
func New(ctx context.Context, cfg Config) (*Reader, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.StorageAccountName)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob client: %w", err)
	}

	var pattern *regexp.Regexp
	if cfg.FilePattern != "" {
		pattern, err = regexp.Compile(cfg.FilePattern)
		if err != nil {
			return nil, fmt.Errorf("invalid file pattern: %w", err)
		}
	}

	return &Reader{
		client:             client,
		storageAccountName: cfg.StorageAccountName,
		containerName:      cfg.ContainerName,
		pattern:            pattern,
		logger:             slog.Default(),
	}, nil
}

// NewWithConnectionString creates a new Reader using a connection string.
func NewWithConnectionString(connStr string, containerName string, filePattern string) (*Reader, error) {
	client, err := azblob.NewClientFromConnectionString(connStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob client from connection string: %w", err)
	}

	// Extract storage account name from connection string
	storageAccountName := extractStorageAccountFromConnStr(connStr)

	var pattern *regexp.Regexp
	if filePattern != "" {
		pattern, err = regexp.Compile(filePattern)
		if err != nil {
			return nil, fmt.Errorf("invalid file pattern: %w", err)
		}
	}

	return &Reader{
		client:             client,
		storageAccountName: storageAccountName,
		containerName:      containerName,
		pattern:            pattern,
		logger:             slog.Default(),
	}, nil
}

// extractStorageAccountFromConnStr extracts the storage account name from a connection string.
func extractStorageAccountFromConnStr(connStr string) string {
	// Connection string format: AccountName=<name>;AccountKey=<key>;...
	for _, part := range regexp.MustCompile(`;`).Split(connStr, -1) {
		if strings.HasPrefix(part, "AccountName=") {
			return strings.TrimPrefix(part, "AccountName=")
		}
	}
	return ""
}

// SetLogger sets the logger for the reader.
func (r *Reader) SetLogger(logger *slog.Logger) {
	if logger != nil {
		r.logger = logger
	}
}

// ListOptions configures the blob listing behavior.
type ListOptions struct {
	SortOrder SortOrder     // Sort order for blobs (default: oldest first)
	Limit     int           // Maximum number of blobs to return (0 = unlimited)
	MinAge    time.Duration // Minimum age of blobs to include (0 = no minimum)
	MaxAge    time.Duration // Maximum age of blobs to include (0 = no maximum)
}

// List returns a list of blobs matching the configured pattern.
func (r *Reader) List(ctx context.Context) ([]BlobInfo, error) {
	return r.ListWithOptions(ctx, ListOptions{})
}

// ListWithOptions returns a list of blobs with custom options for sorting and limiting.
func (r *Reader) ListWithOptions(ctx context.Context, opts ListOptions) ([]BlobInfo, error) {
	r.logger.Debug("listing blobs from container",
		"container", r.containerName,
		"sort_order", opts.SortOrder,
		"limit", opts.Limit,
		"min_age", opts.MinAge,
		"max_age", opts.MaxAge,
	)

	var blobs []BlobInfo
	now := time.Now()

	pager := r.client.NewListBlobsFlatPager(r.containerName, &container.ListBlobsFlatOptions{})

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		for _, blobItem := range resp.Segment.BlobItems {
			name := *blobItem.Name

			// Apply pattern filter if configured
			if r.pattern != nil && !r.pattern.MatchString(name) {
				continue
			}

			var size int64
			if blobItem.Properties.ContentLength != nil {
				size = *blobItem.Properties.ContentLength
			}

			var contentType string
			if blobItem.Properties.ContentType != nil {
				contentType = *blobItem.Properties.ContentType
			}

			var lastModified time.Time
			if blobItem.Properties.LastModified != nil {
				lastModified = *blobItem.Properties.LastModified
			}

			// Apply age filters
			if !lastModified.IsZero() {
				age := now.Sub(lastModified)

				// Skip if blob is too new (hasn't reached minimum age)
				if opts.MinAge > 0 && age < opts.MinAge {
					continue
				}

				// Skip if blob is too old (exceeds maximum age)
				if opts.MaxAge > 0 && age > opts.MaxAge {
					continue
				}
			}

			blobs = append(blobs, BlobInfo{
				Name:         name,
				Size:         size,
				ContentType:  contentType,
				LastModified: lastModified,
			})
		}
	}

	// Sort blobs by last modified time
	switch opts.SortOrder {
	case SortNewestFirst:
		sort.Slice(blobs, func(i, j int) bool {
			return blobs[i].LastModified.After(blobs[j].LastModified)
		})
	case SortOldestFirst, "":
		sort.Slice(blobs, func(i, j int) bool {
			return blobs[i].LastModified.Before(blobs[j].LastModified)
		})
	}

	// Apply limit if specified
	if opts.Limit > 0 && len(blobs) > opts.Limit {
		blobs = blobs[:opts.Limit]
	}

	return blobs, nil
}

// Download downloads a blob and returns its content as an io.ReadCloser.
func (r *Reader) Download(ctx context.Context, blobName string) (io.ReadCloser, int64, error) {
	resp, err := r.client.DownloadStream(ctx, r.containerName, blobName, &blob.DownloadStreamOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download blob %s: %w", blobName, err)
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	return resp.Body, size, nil
}

// Delete removes a blob from the container.
func (r *Reader) Delete(ctx context.Context, blobName string) error {
	_, err := r.client.DeleteBlob(ctx, r.containerName, blobName, nil)
	if err != nil {
		return fmt.Errorf("failed to delete blob %s: %w", blobName, err)
	}
	return nil
}

// GetContainerName returns the container name.
func (r *Reader) GetContainerName() string {
	return r.containerName
}

// GetStorageAccountName returns the storage account name.
func (r *Reader) GetStorageAccountName() string {
	return r.storageAccountName
}
