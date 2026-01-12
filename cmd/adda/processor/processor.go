// Package processor implements the main processing pipeline.
package processor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/steigr/azure-diagnostic-data-adapter/cmd/adda/config"
	"github.com/steigr/azure-diagnostic-data-adapter/internal/utils"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/enricher"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/metrics"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/external"
	jsonparser "github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/json"
	ndjsonparser "github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/ndjson"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/reader"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/writer"
)

// Processor orchestrates the blob processing pipeline.
type Processor struct {
	cfg      *config.Config
	readers  []*reader.Reader // One reader per container
	parsers  *parser.Registry
	enricher *enricher.Enricher
	writer   *writer.Writer
	metrics  *metrics.Metrics
	logger   *slog.Logger
}

// New creates a new Processor.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Processor, error) {
	// Get all container names to process
	containerNames := cfg.Source.GetContainerNames()
	if len(containerNames) == 0 {
		return nil, fmt.Errorf("no container names configured")
	}

	// Create readers for each container
	var readers []*reader.Reader
	for _, containerName := range containerNames {
		var r *reader.Reader
		var err error
		if cfg.Source.ConnectionString != "" {
			r, err = reader.NewWithConnectionString(
				cfg.Source.ConnectionString,
				containerName,
				cfg.Source.FilePattern,
			)
		} else {
			r, err = reader.New(ctx, reader.Config{
				StorageAccountName: cfg.Source.StorageAccountName,
				ContainerName:      containerName,
				FilePattern:        cfg.Source.FilePattern,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create reader for container %s: %w", containerName, err)
		}

		// Set logger for the reader
		r.SetLogger(logger)
		readers = append(readers, r)
	}

	logger.Info("created readers for containers", "containers", containerNames, "count", len(readers))

	// Create parser registry
	parsers := parser.NewRegistry()
	for _, pcfg := range cfg.Parsers {
		var p parser.Parser
		var err error
		switch pcfg.Type {
		case "ndjson":
			np, err := ndjsonparser.New(pcfg.ID, pcfg.FilePattern)
			if err != nil {
				return nil, fmt.Errorf("failed to create ndjson parser %s: %w", pcfg.ID, err)
			}
			np.SetLogger(logger)
			p = np
		case "json":
			jp, err := jsonparser.New(pcfg.ID, pcfg.FilePattern)
			if err != nil {
				return nil, fmt.Errorf("failed to create json parser %s: %w", pcfg.ID, err)
			}
			jp.SetLogger(logger)
			p = jp
		case "external":
			p, err = external.New(external.Config{
				ID:          pcfg.ID,
				FilePattern: pcfg.FilePattern,
				Command:     pcfg.Command,
				Args:        pcfg.Args,
				Env:         pcfg.Env,
				TempDir:     cfg.Processing.TempDir,
				Stdin:       pcfg.Stdin,
				Stdout:      pcfg.Stdout,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create external parser %s: %w", pcfg.ID, err)
			}
		default:
			return nil, fmt.Errorf("unknown parser type: %s", pcfg.Type)
		}
		parsers.Register(p)
	}

	// Create enricher
	e, err := enricher.New(cfg.EnrichmentTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to create enricher: %w", err)
	}

	// Create writer
	if err := cfg.EnsureOutputDir(); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	w := writer.New(cfg.GetWriterConfig())

	// Create metrics
	m := metrics.New()

	proc := &Processor{
		cfg:      cfg,
		readers:  readers,
		parsers:  parsers,
		enricher: e,
		writer:   w,
		metrics:  m,
		logger:   logger,
	}

	// Perform startup disk space check if backoff is enabled
	if cfg.Processing.BackoffEnabled {
		if err := proc.checkDiskSpaceStartup(); err != nil {
			return nil, err
		}
	}

	return proc, nil
}

// checkDiskSpaceStartup checks disk space at startup and logs a warning if low.
func (p *Processor) checkDiskSpaceStartup() error {
	freeSpace, err := utils.GetFreeSpace(p.cfg.Output.Directory)
	if err != nil {
		p.logger.Warn("failed to check disk space at startup", "error", err)
		return nil // Don't fail startup, just warn
	}

	p.metrics.SetOutputDirFreeBytes(freeSpace)
	minFreeBytes := p.cfg.Processing.GetMinFreeSpaceBytes()

	p.logger.Info("disk space check",
		"output_directory", p.cfg.Output.Directory,
		"free_space_bytes", freeSpace,
		"free_space_mb", float64(freeSpace)/(1024*1024),
		"min_free_space_bytes", minFreeBytes,
		"min_free_space_mb", float64(minFreeBytes)/(1024*1024),
	)

	if freeSpace < minFreeBytes {
		p.logger.Warn("low disk space at startup - processing will be paused until space is available",
			"free_space_mb", float64(freeSpace)/(1024*1024),
			"min_free_space_mb", float64(minFreeBytes)/(1024*1024),
		)
		p.metrics.RecordBackoff()
	}

	return nil
}

// CheckDiskSpace checks if there is sufficient disk space for processing.
// Returns true if there is enough space, false if backoff should occur.
func (p *Processor) CheckDiskSpace() (bool, error) {
	if !p.cfg.Processing.BackoffEnabled {
		return true, nil
	}

	freeSpace, err := utils.GetFreeSpace(p.cfg.Output.Directory)
	if err != nil {
		return true, err // Continue on error, just warn
	}

	p.metrics.SetOutputDirFreeBytes(freeSpace)
	minFreeBytes := p.cfg.Processing.GetMinFreeSpaceBytes()

	if freeSpace < minFreeBytes {
		return false, nil
	}

	p.metrics.ClearBackoff()
	return true, nil
}

// Run starts the processing loop.
func (p *Processor) Run(ctx context.Context) error {
	// Use configured poll interval, default to 1 minute if not set
	pollInterval := p.cfg.Processing.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Minute
	}

	p.logger.Info("starting processor",
		"workers", p.cfg.Processing.Workers,
		"dry_run", p.cfg.Processing.DryRun,
		"poll_interval", pollInterval,
		"backoff_enabled", p.cfg.Processing.BackoffEnabled,
		"min_free_space_bytes", p.cfg.Processing.GetMinFreeSpaceBytes(),
	)

	// Create ticker for polling
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Process immediately on start (if disk space allows)
	if hasSpace, err := p.CheckDiskSpace(); err != nil {
		p.logger.Warn("failed to check disk space", "error", err)
	} else if hasSpace {
		if err := p.processOnce(ctx); err != nil {
			p.logger.Error("processing error", "error", err)
		}
	} else {
		p.logger.Warn("insufficient disk space, waiting for space to become available")
	}

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("processor stopped")
			return ctx.Err()
		case <-ticker.C:
			// Check disk space before processing
			hasSpace, err := p.CheckDiskSpace()
			if err != nil {
				p.logger.Warn("failed to check disk space", "error", err)
			}

			if !hasSpace {
				freeSpace, _ := utils.GetFreeSpace(p.cfg.Output.Directory)
				p.logger.Warn("insufficient disk space, skipping processing cycle",
					"free_space_mb", float64(freeSpace)/(1024*1024),
					"min_free_space_mb", float64(p.cfg.Processing.GetMinFreeSpaceBytes())/(1024*1024),
				)
				p.metrics.RecordBackoff()
				continue
			}

			if err := p.processOnce(ctx); err != nil {
				p.logger.Error("processing error", "error", err)
			}
		}
	}
}

// RunOnce processes all available blobs once and returns.
func (p *Processor) RunOnce(ctx context.Context) error {
	// Check disk space before processing
	hasSpace, err := p.CheckDiskSpace()
	if err != nil {
		p.logger.Warn("failed to check disk space", "error", err)
	}

	if !hasSpace {
		freeSpace, _ := utils.GetFreeSpace(p.cfg.Output.Directory)
		p.logger.Warn("insufficient disk space, cannot process",
			"free_space_mb", float64(freeSpace)/(1024*1024),
			"min_free_space_mb", float64(p.cfg.Processing.GetMinFreeSpaceBytes())/(1024*1024),
		)
		p.metrics.RecordBackoff()
		return fmt.Errorf("insufficient disk space: %.2f MB free, %.2f MB required",
			float64(freeSpace)/(1024*1024),
			float64(p.cfg.Processing.GetMinFreeSpaceBytes())/(1024*1024))
	}

	// Use once_limit for single-shot mode
	limit := p.cfg.Processing.OnceLimit
	if limit <= 0 {
		limit = 10 // Default to 10 if not set or invalid
	}
	return p.processOnceWithLimit(ctx, limit)
}

// DryRun lists blobs, shows which parser would be used, and displays enrichment preview.
func (p *Processor) DryRun(ctx context.Context) error {
	p.logger.Info("=== DRY RUN MODE ===")
	p.logger.Info("listing blobs from storage",
		"storage_account", p.cfg.Source.StorageAccountName,
		"containers", p.cfg.Source.GetContainerNames(),
		"file_pattern", p.cfg.Source.FilePattern,
	)

	// Show available parsers
	p.logger.Info("configured parsers", "parsers", p.parsers.List())

	totalBlobs := 0
	for _, rdr := range p.readers {
		containerName := rdr.GetContainerName()
		p.logger.Info("checking container", "container", containerName)

		// List blobs
		blobs, err := rdr.List(ctx)
		if err != nil {
			p.logger.Error("failed to list blobs", "container", containerName, "error", err)
			continue
		}

		if len(blobs) == 0 {
			p.logger.Info("no blobs found matching the pattern", "container", containerName)
			continue
		}

		p.logger.Info("found blobs in container", "container", containerName, "count", len(blobs))
		totalBlobs += len(blobs)

		// Process each blob in dry-run mode
		for _, blob := range blobs {
			p.dryRunBlob(blob, containerName)
		}
	}

	p.logger.Info("total blobs found", "count", totalBlobs)

	// Show enrichment template preview
	p.showEnrichmentPreview()

	// Show output configuration
	p.logger.Info("output configuration",
		"directory", p.cfg.Output.Directory,
		"filename", p.cfg.Output.Filename,
		"max_size_mb", p.cfg.Output.MaxSize,
		"max_backups", p.cfg.Output.MaxBackups,
		"max_age_days", p.cfg.Output.MaxAge,
	)

	p.logger.Info("=== DRY RUN COMPLETE ===")
	return nil
}

func (p *Processor) dryRunBlob(blob reader.BlobInfo, containerName string) {
	// Find matching parser
	psr, ok := p.parsers.FindMatching(blob.Name)

	if !ok {
		p.logger.Warn("blob has no matching parser",
			"container", containerName,
			"blob", blob.Name,
			"size", blob.Size,
			"content_type", blob.ContentType,
			"last_modified", blob.LastModified,
		)
		return
	}

	// Get file match capture groups
	fileMatch := psr.MatchResult(blob.Name)

	logFields := []any{
		"container", containerName,
		"blob", blob.Name,
		"size", blob.Size,
		"content_type", blob.ContentType,
		"last_modified", blob.LastModified,
		"parser", psr.ID(),
	}

	if len(fileMatch) > 0 {
		logFields = append(logFields, "file_match", fileMatch)
	}

	p.logger.Info("blob would be processed", logFields...)
}

func (p *Processor) showEnrichmentPreview() {
	if p.cfg.EnrichmentTemplate == "" {
		p.logger.Info("no enrichment template configured")
		return
	}

	// Get container name for preview (use first container)
	containerNames := p.cfg.Source.GetContainerNames()
	containerName := ""
	if len(containerNames) > 0 {
		containerName = containerNames[0]
	}

	// Create sample file match for preview
	sampleFileMatch := map[string]string{
		"year":  "2026",
		"month": "01",
		"day":   "12",
	}

	// Create sample metadata for preview using NewMetadataWithFileMatch for proper path parsing
	sampleMetadata := enricher.NewMetadataWithFileMatch(
		"example/path/sample-file.json",
		containerName,
		p.cfg.Source.StorageAccountName,
		time.Now(),
		1024,
		"application/json",
		time.Now().Add(-1*time.Hour), // Sample: 1 hour ago
		sampleFileMatch,
	)

	// Create sample record
	sampleRecord := map[string]any{
		"example_field": "example_value",
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	// Apply enrichment
	enriched, err := p.enricher.Enrich(sampleRecord, sampleMetadata)
	if err != nil {
		p.logger.Error("enrichment template error", "error", err)
		return
	}

	p.logger.Info("enrichment preview (sample record)",
		"original_record", sampleRecord,
		"metadata", map[string]any{
			"blob_name":       sampleMetadata.BlobName,
			"container_name":  sampleMetadata.ContainerName,
			"storage_account": sampleMetadata.StorageAccount,
			"processed_at":    sampleMetadata.ProcessedAt.Format(time.RFC3339),
			"size":            sampleMetadata.Size,
			"content_type":    sampleMetadata.ContentType,
			"last_modified":   sampleMetadata.LastModified.Format(time.RFC3339),
			"directory":       sampleMetadata.Directory,
			"file_name":       sampleMetadata.FileName,
			"extension":       sampleMetadata.Extension,
			"path_parts":      sampleMetadata.PathParts,
			"file_match":      sampleMetadata.FileMatch,
		},
		"enriched_record", enriched,
	)
}

func (p *Processor) processOnce(ctx context.Context) error {
	return p.processOnceWithLimit(ctx, p.cfg.Processing.BatchLimit)
}

// blobWithReader associates a blob with its reader for processing
type blobWithReader struct {
	blob   reader.BlobInfo
	reader *reader.Reader
}

func (p *Processor) processOnceWithLimit(ctx context.Context, limit int) error {
	// Update free space metric
	if freeSpace, err := utils.GetFreeSpace(p.cfg.Output.Directory); err == nil {
		p.metrics.SetOutputDirFreeBytes(freeSpace)
	}

	// Determine sort order
	sortOrder := reader.SortOldestFirst
	if p.cfg.Processing.SortOrder == "newest" {
		sortOrder = reader.SortNewestFirst
	}

	// Collect blobs from all readers
	var allBlobs []blobWithReader
	for _, rdr := range p.readers {
		blobs, err := rdr.ListWithOptions(ctx, reader.ListOptions{
			SortOrder: sortOrder,
			Limit:     0, // Don't limit per-container, we'll limit total
			MinAge:    p.cfg.Processing.MinAge,
			MaxAge:    p.cfg.Processing.MaxAge,
		})
		if err != nil {
			p.logger.Error("failed to list blobs", "container", rdr.GetContainerName(), "error", err)
			continue
		}
		for _, blob := range blobs {
			allBlobs = append(allBlobs, blobWithReader{blob: blob, reader: rdr})
		}
	}

	// Record poll metrics
	foundData := len(allBlobs) > 0
	p.metrics.RecordPoll(foundData)

	if len(allBlobs) == 0 {
		p.logger.Debug("no blobs to process")
		return nil
	}

	// Sort all blobs by last modified time
	if sortOrder == reader.SortNewestFirst {
		sort.Slice(allBlobs, func(i, j int) bool {
			return allBlobs[i].blob.LastModified.After(allBlobs[j].blob.LastModified)
		})
	} else {
		sort.Slice(allBlobs, func(i, j int) bool {
			return allBlobs[i].blob.LastModified.Before(allBlobs[j].blob.LastModified)
		})
	}

	// Apply limit if specified
	if limit > 0 && len(allBlobs) > limit {
		allBlobs = allBlobs[:limit]
	}

	p.logger.Info("found blobs to process",
		"count", len(allBlobs),
		"sort_order", p.cfg.Processing.SortOrder,
		"limit", limit,
		"min_age", p.cfg.Processing.MinAge,
		"max_age", p.cfg.Processing.MaxAge,
	)

	// Process blobs
	if p.cfg.Processing.Workers <= 1 {
		// Sequential processing
		for _, bwr := range allBlobs {
			if err := p.processBlob(ctx, bwr.blob, bwr.reader); err != nil {
				p.logger.Error("failed to process blob", "container", bwr.reader.GetContainerName(), "blob", bwr.blob.Name, "error", err)
				p.metrics.RecordBlobFailed()
			}
		}
	} else {
		// Parallel processing
		p.processParallel(ctx, allBlobs)
	}

	return nil
}

func (p *Processor) processParallel(ctx context.Context, blobs []blobWithReader) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.cfg.Processing.Workers)

	for _, bwr := range blobs {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(b blobWithReader) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := p.processBlob(ctx, b.blob, b.reader); err != nil {
				p.logger.Error("failed to process blob", "container", b.reader.GetContainerName(), "blob", b.blob.Name, "error", err)
				p.metrics.RecordBlobFailed()
			}
		}(bwr)
	}

	wg.Wait()
}

func (p *Processor) processBlob(ctx context.Context, blob reader.BlobInfo, rdr *reader.Reader) error {
	start := time.Now()
	containerName := rdr.GetContainerName()
	storageAccountName := rdr.GetStorageAccountName()
	p.logger.Debug("processing blob", "container", containerName, "name", blob.Name, "size", blob.Size)

	// Download blob
	data, size, err := rdr.Download(ctx, blob.Name)
	if err != nil {
		return fmt.Errorf("failed to download blob: %w", err)
	}
	defer func() { _ = data.Close() }()

	// Read all data into memory for parser
	buf, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read blob data: %w", err)
	}

	// Find a parser that matches the blob name
	psr, ok := p.parsers.FindMatching(blob.Name)
	if !ok {
		p.logger.Debug("no matching parser for blob", "container", containerName, "name", blob.Name)
		return nil
	}

	p.logger.Debug("using parser", "parser", psr.ID(), "container", containerName, "blob", blob.Name)

	p.metrics.SetActiveParsers(1)
	defer p.metrics.SetActiveParsers(0)

	// Get file match capture groups from parser
	fileMatch := psr.MatchResult(blob.Name)

	// Create parse context with storage information
	parseCtx := parser.ParseContext{
		BlobName:           blob.Name,
		ContainerName:      containerName,
		StorageAccountName: storageAccountName,
		FileMatch:          fileMatch,
	}

	// Parse data with context
	records, err := psr.ParseWithContext(bytes.NewReader(buf), parseCtx)
	if err != nil {
		return fmt.Errorf("failed to parse blob: %w", err)
	}

	if len(records) == 0 {
		p.logger.Debug("no records parsed from blob", "container", containerName, "name", blob.Name)
		return nil
	}

	// Enrich records with file match data
	metadata := enricher.NewMetadataWithFileMatch(
		blob.Name,
		containerName,
		storageAccountName,
		time.Now(),
		size,
		blob.ContentType,
		blob.LastModified,
		fileMatch,
	)

	enrichedRecords, err := p.enricher.EnrichBatch(records, metadata)
	if err != nil {
		return fmt.Errorf("failed to enrich records: %w", err)
	}

	// Write records
	written, err := p.writer.WriteBatch(enrichedRecords)
	if err != nil {
		return fmt.Errorf("failed to write records: %w", err)
	}

	p.metrics.RecordLinesWritten(written)

	// Delete blob if configured
	if p.cfg.Processing.DeleteAfterProcess {
		if err := rdr.Delete(ctx, blob.Name); err != nil {
			return fmt.Errorf("failed to delete blob: %w", err)
		}
		p.logger.Debug("deleted blob", "container", containerName, "name", blob.Name)
	}

	// Record metrics
	elapsed := time.Since(start)
	p.metrics.BlobProcessingTime.Observe(elapsed.Seconds())
	p.metrics.RecordBlobProcessed(size)

	p.logger.Info("processed blob",
		"container", containerName,
		"name", blob.Name,
		"records", len(enrichedRecords),
		"duration", elapsed,
	)

	return nil
}

// Close closes the processor and releases resources.
func (p *Processor) Close() error {
	return p.writer.Close()
}
