// Package processor implements the main processing pipeline.
package processor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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
	reader   *reader.Reader
	parsers  *parser.Registry
	enricher *enricher.Enricher
	writer   *writer.Writer
	metrics  *metrics.Metrics
	logger   *slog.Logger
}

// New creates a new Processor.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Processor, error) {
	// Create reader
	var r *reader.Reader
	var err error
	if cfg.Source.ConnectionString != "" {
		r, err = reader.NewWithConnectionString(
			cfg.Source.ConnectionString,
			cfg.Source.ContainerName,
			cfg.Source.FilePattern,
		)
	} else {
		r, err = reader.New(ctx, reader.Config{
			StorageAccountName: cfg.Source.StorageAccountName,
			ContainerName:      cfg.Source.ContainerName,
			FilePattern:        cfg.Source.FilePattern,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

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

	return &Processor{
		cfg:      cfg,
		reader:   r,
		parsers:  parsers,
		enricher: e,
		writer:   w,
		metrics:  m,
		logger:   logger,
	}, nil
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
	)

	// Create ticker for polling
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Process immediately on start
	if err := p.processOnce(ctx); err != nil {
		p.logger.Error("processing error", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("processor stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := p.processOnce(ctx); err != nil {
				p.logger.Error("processing error", "error", err)
			}
		}
	}
}

// RunOnce processes all available blobs once and returns.
func (p *Processor) RunOnce(ctx context.Context) error {
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
		"container", p.cfg.Source.ContainerName,
		"file_pattern", p.cfg.Source.FilePattern,
	)

	// List blobs
	blobs, err := p.reader.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list blobs: %w", err)
	}

	if len(blobs) == 0 {
		p.logger.Info("no blobs found matching the pattern")
		return nil
	}

	p.logger.Info("found blobs", "count", len(blobs))

	// Show available parsers
	p.logger.Info("configured parsers", "parsers", p.parsers.List())

	// Process each blob in dry-run mode
	for _, blob := range blobs {
		p.dryRunBlob(blob)
	}

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

func (p *Processor) dryRunBlob(blob reader.BlobInfo) {
	// Find matching parser
	psr, ok := p.parsers.FindMatching(blob.Name)

	if !ok {
		p.logger.Warn("blob has no matching parser",
			"blob", blob.Name,
			"size", blob.Size,
			"content_type", blob.ContentType,
			"last_modified", blob.LastModified,
		)
		return
	}

	p.logger.Info("blob would be processed",
		"blob", blob.Name,
		"size", blob.Size,
		"content_type", blob.ContentType,
		"last_modified", blob.LastModified,
		"parser", psr.ID(),
	)
}

func (p *Processor) showEnrichmentPreview() {
	if p.cfg.EnrichmentTemplate == "" {
		p.logger.Info("no enrichment template configured")
		return
	}

	// Create sample metadata for preview using NewMetadata for proper path parsing
	sampleMetadata := enricher.NewMetadata(
		"example/path/sample-file.json",
		p.cfg.Source.ContainerName,
		p.cfg.Source.StorageAccountName,
		time.Now(),
		1024,
		"application/json",
		time.Now().Add(-1*time.Hour), // Sample: 1 hour ago
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
		},
		"enriched_record", enriched,
	)
}

func (p *Processor) processOnce(ctx context.Context) error {
	return p.processOnceWithLimit(ctx, p.cfg.Processing.BatchLimit)
}

func (p *Processor) processOnceWithLimit(ctx context.Context, limit int) error {
	// Check free space if backoff is enabled
	if p.cfg.Processing.BackoffEnabled {
		freeSpace, err := utils.GetFreeSpace(p.cfg.Output.Directory)
		if err != nil {
			p.logger.Warn("failed to get free space", "error", err)
		} else {
			p.metrics.SetOutputDirFreeBytes(freeSpace)
			minFreeBytes := uint64(p.cfg.Processing.MinFreeSpaceGB) * 1024 * 1024 * 1024
			if freeSpace < minFreeBytes {
				p.logger.Warn("insufficient free space, backing off",
					"free_space_gb", float64(freeSpace)/(1024*1024*1024),
					"min_free_space_gb", p.cfg.Processing.MinFreeSpaceGB,
				)
				return nil
			}
		}
	}

	// Determine sort order
	sortOrder := reader.SortOldestFirst
	if p.cfg.Processing.SortOrder == "newest" {
		sortOrder = reader.SortNewestFirst
	}

	// List blobs with options
	blobs, err := p.reader.ListWithOptions(ctx, reader.ListOptions{
		SortOrder: sortOrder,
		Limit:     limit,
		MinAge:    p.cfg.Processing.MinAge,
		MaxAge:    p.cfg.Processing.MaxAge,
	})
	if err != nil {
		p.metrics.RecordPoll(false)
		return fmt.Errorf("failed to list blobs: %w", err)
	}

	// Record poll metrics
	foundData := len(blobs) > 0
	p.metrics.RecordPoll(foundData)

	if len(blobs) == 0 {
		p.logger.Debug("no blobs to process")
		return nil
	}

	p.logger.Info("found blobs to process",
		"count", len(blobs),
		"sort_order", p.cfg.Processing.SortOrder,
		"limit", limit,
		"min_age", p.cfg.Processing.MinAge,
		"max_age", p.cfg.Processing.MaxAge,
	)

	// Process blobs
	if p.cfg.Processing.Workers <= 1 {
		// Sequential processing
		for _, blob := range blobs {
			if err := p.processBlob(ctx, blob); err != nil {
				p.logger.Error("failed to process blob", "blob", blob.Name, "error", err)
				p.metrics.RecordBlobFailed()
			}
		}
	} else {
		// Parallel processing
		p.processParallel(ctx, blobs)
	}

	return nil
}

func (p *Processor) processParallel(ctx context.Context, blobs []reader.BlobInfo) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.cfg.Processing.Workers)

	for _, blob := range blobs {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(b reader.BlobInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := p.processBlob(ctx, b); err != nil {
				p.logger.Error("failed to process blob", "blob", b.Name, "error", err)
				p.metrics.RecordBlobFailed()
			}
		}(blob)
	}

	wg.Wait()
}

func (p *Processor) processBlob(ctx context.Context, blob reader.BlobInfo) error {
	start := time.Now()
	p.logger.Debug("processing blob", "name", blob.Name, "size", blob.Size)

	// Download blob
	data, size, err := p.reader.Download(ctx, blob.Name)
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
		p.logger.Debug("no matching parser for blob", "name", blob.Name)
		return nil
	}

	p.logger.Debug("using parser", "parser", psr.ID(), "blob", blob.Name)

	p.metrics.SetActiveParsers(1)
	defer p.metrics.SetActiveParsers(0)

	// Parse data
	records, err := psr.Parse(bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("failed to parse blob: %w", err)
	}

	if len(records) == 0 {
		p.logger.Debug("no records parsed from blob", "name", blob.Name)
		return nil
	}

	// Enrich records
	metadata := enricher.NewMetadata(
		blob.Name,
		p.reader.GetContainerName(),
		p.cfg.Source.StorageAccountName,
		time.Now(),
		size,
		blob.ContentType,
		blob.LastModified,
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
		if err := p.reader.Delete(ctx, blob.Name); err != nil {
			return fmt.Errorf("failed to delete blob: %w", err)
		}
		p.logger.Debug("deleted blob", "name", blob.Name)
	}

	// Record metrics
	elapsed := time.Since(start)
	p.metrics.BlobProcessingTime.Observe(elapsed.Seconds())
	p.metrics.RecordBlobProcessed(size)

	p.logger.Info("processed blob",
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
