// Package main provides the entry point for the Azure Diagnostic Data Adapter.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/steigr/azure-diagnostic-data-adapter/cmd/adda/config"
	"github.com/steigr/azure-diagnostic-data-adapter/cmd/adda/processor"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/metrics"
)

var (
	cfgFile   string
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "adda",
		Short:   "Azure Diagnostic Data Adapter",
		Long:    `A tool to read data from Azure Storage Account blob containers, parse, enrich, and write to NDJSON files.`,
		Version: version,
		RunE:    run,
	}

	// Flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml)")

	// Source flags
	rootCmd.Flags().String("storage-account", "", "Azure Storage Account name")
	rootCmd.Flags().String("container", "", "Blob container name")
	rootCmd.Flags().String("file-pattern", "", "Regex pattern to match files")
	rootCmd.Flags().String("connection-string", "", "Azure Storage connection string")

	// Output flags
	rootCmd.Flags().String("output-dir", ".", "Output directory for NDJSON files")
	rootCmd.Flags().String("output-file", "output.ndjson", "Output filename")
	rootCmd.Flags().Int("max-size", 100, "Max file size in MB before rotation")
	rootCmd.Flags().Int("max-backups", 3, "Max number of backup files")
	rootCmd.Flags().Int("max-age", 28, "Max age in days for backup files")
	rootCmd.Flags().Bool("gzip", false, "Enable gzip compression for output")

	// Processing flags
	rootCmd.Flags().Int("workers", 1, "Number of parallel workers")
	rootCmd.Flags().Bool("dry-run", false, "Run without making changes")
	rootCmd.Flags().Bool("delete-after-process", true, "Delete blobs after processing")
	rootCmd.Flags().Bool("once", false, "Process once and exit")
	rootCmd.Flags().String("temp-dir", "", "Temporary directory for external parsers (default: system temp)")
	rootCmd.Flags().String("sort-order", "oldest", "Blob processing order: 'oldest' or 'newest'")
	rootCmd.Flags().Int("batch-limit", 0, "Max blobs per batch (0 = unlimited)")
	rootCmd.Flags().Int("once-limit", 10, "Max blobs for --once mode")
	rootCmd.Flags().Duration("blob-min-age", 0, "Minimum age of blobs to process (e.g., 15s, 5m, 1h)")
	rootCmd.Flags().Duration("blob-max-age", 0, "Maximum age of blobs to process (e.g., 24h, 168h)")
	rootCmd.Flags().Duration("poll-interval", time.Minute, "Interval between polling for new blobs (e.g., 30s, 1m, 5m)")

	// Metrics flags
	rootCmd.Flags().Bool("metrics", true, "Enable Prometheus metrics")
	rootCmd.Flags().String("metrics-address", ":9090", "Metrics server address")

	// Logging flags
	rootCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.Flags().String("log-format", "text", "Log format (text, json)")

	// Bind flags to viper
	_ = viper.BindPFlag("source.storage_account_name", rootCmd.Flags().Lookup("storage-account"))
	_ = viper.BindPFlag("source.container_name", rootCmd.Flags().Lookup("container"))
	_ = viper.BindPFlag("source.file_pattern", rootCmd.Flags().Lookup("file-pattern"))
	_ = viper.BindPFlag("source.connection_string", rootCmd.Flags().Lookup("connection-string"))
	_ = viper.BindPFlag("output.directory", rootCmd.Flags().Lookup("output-dir"))
	_ = viper.BindPFlag("output.filename", rootCmd.Flags().Lookup("output-file"))
	_ = viper.BindPFlag("output.max_size", rootCmd.Flags().Lookup("max-size"))
	_ = viper.BindPFlag("output.max_backups", rootCmd.Flags().Lookup("max-backups"))
	_ = viper.BindPFlag("output.max_age", rootCmd.Flags().Lookup("max-age"))
	_ = viper.BindPFlag("output.gzip", rootCmd.Flags().Lookup("gzip"))
	_ = viper.BindPFlag("processing.workers", rootCmd.Flags().Lookup("workers"))
	_ = viper.BindPFlag("processing.dry_run", rootCmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("processing.delete_after_process", rootCmd.Flags().Lookup("delete-after-process"))
	_ = viper.BindPFlag("processing.temp_dir", rootCmd.Flags().Lookup("temp-dir"))
	_ = viper.BindPFlag("processing.sort_order", rootCmd.Flags().Lookup("sort-order"))
	_ = viper.BindPFlag("processing.batch_limit", rootCmd.Flags().Lookup("batch-limit"))
	_ = viper.BindPFlag("processing.once_limit", rootCmd.Flags().Lookup("once-limit"))
	_ = viper.BindPFlag("processing.min_age", rootCmd.Flags().Lookup("blob-min-age"))
	_ = viper.BindPFlag("processing.max_age", rootCmd.Flags().Lookup("blob-max-age"))
	_ = viper.BindPFlag("processing.poll_interval", rootCmd.Flags().Lookup("poll-interval"))
	_ = viper.BindPFlag("metrics.enabled", rootCmd.Flags().Lookup("metrics"))
	_ = viper.BindPFlag("metrics.address", rootCmd.Flags().Lookup("metrics-address"))
	_ = viper.BindPFlag("logging.level", rootCmd.Flags().Lookup("log-level"))
	_ = viper.BindPFlag("logging.format", rootCmd.Flags().Lookup("log-format"))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup logger
	logger := setupLogger(cfg)
	logger.Info("starting Azure Diagnostic Data Adapter",
		"version", version,
		"buildTime", buildTime,
		"gitCommit", gitCommit,
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("received shutdown signal")
		cancel()
	}()

	// Start metrics server if enabled
	if cfg.Metrics.Enabled {
		go func() {
			logger.Info("starting metrics server", "address", cfg.Metrics.Address)
			mux := http.NewServeMux()
			mux.Handle("/metrics", metrics.Handler())
			mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte("ok")); err != nil {
					logger.Error("failed to write health response", "error", err)
				}
			})
			server := &http.Server{
				Addr:    cfg.Metrics.Address,
				Handler: mux,
			}
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server error", "error", err)
			}
		}()
	}

	// Create processor
	proc, err := processor.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create processor: %w", err)
	}
	defer func() {
		if err := proc.Close(); err != nil {
			logger.Error("failed to close processor", "error", err)
		}
	}()

	// Check for --dry-run flag (implies --once)
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("failed to get dry-run flag: %w", err)
	}
	if dryRun {
		logger.Info("running in dry-run mode")
		if err := proc.DryRun(ctx); err != nil {
			return fmt.Errorf("dry-run failed: %w", err)
		}
		return nil
	}

	// Check for --once flag
	once, err := cmd.Flags().GetBool("once")
	if err != nil {
		return fmt.Errorf("failed to get once flag: %w", err)
	}
	if once {
		logger.Info("running in single-shot mode")
		if err := proc.RunOnce(ctx); err != nil {
			return fmt.Errorf("single-shot processing failed: %w", err)
		}
		return nil
	}

	// Run processor loop
	if err := proc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("processor failed: %w", err)
	}
	return nil
}

func setupLogger(cfg *config.Config) *slog.Logger {
	var handler slog.Handler

	level := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
