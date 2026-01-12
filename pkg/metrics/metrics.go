// Package metrics provides Prometheus metrics for the Azure Diagnostic Data Reader.
package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	once     sync.Once
	instance *Metrics
)

// Metrics holds all Prometheus metrics for the application.
type Metrics struct {
	BlobsProcessed     prometheus.Counter
	BlobsFailed        prometheus.Counter
	ProcessedBytes     prometheus.Counter
	LinesWritten       prometheus.Counter
	BlobProcessingTime prometheus.Histogram
	OutputDirFreeBytes prometheus.Gauge
	ActiveParsers      prometheus.Gauge
	PollsTotal         prometheus.Counter
	PollsWithData      prometheus.Counter
	PollsEmpty         prometheus.Counter
}

// New creates and registers all metrics. It returns a singleton instance.
func New() *Metrics {
	once.Do(func() {
		instance = &Metrics{
			BlobsProcessed: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_blobs_processed_total",
				Help: "Total number of successfully processed blobs",
			}),
			BlobsFailed: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_blobs_failed_total",
				Help: "Total number of failed blob processing attempts",
			}),
			ProcessedBytes: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_processed_bytes_total",
				Help: "Total size of processed data in bytes",
			}),
			LinesWritten: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_lines_written_total",
				Help: "Total number of lines (items) written to output files",
			}),
			BlobProcessingTime: promauto.NewHistogram(prometheus.HistogramOpts{
				Name:    "adda_blob_processing_seconds",
				Help:    "Time taken for processing each blob in seconds",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 15),
			}),
			OutputDirFreeBytes: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "adda_output_dir_free_bytes",
				Help: "Current free space in output directory in bytes",
			}),
			ActiveParsers: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "adda_active_parsers",
				Help: "Number of currently active parsers",
			}),
			PollsTotal: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_polls_total",
				Help: "Total number of times the reader checked for available blobs",
			}),
			PollsWithData: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_polls_with_data_total",
				Help: "Number of polls that found data to process",
			}),
			PollsEmpty: promauto.NewCounter(prometheus.CounterOpts{
				Name: "adda_polls_empty_total",
				Help: "Number of polls that found no data to process",
			}),
		}
	})
	return instance
}

// Handler returns the Prometheus HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordBlobProcessed increments the processed blob counter and adds bytes.
func (m *Metrics) RecordBlobProcessed(bytes int64) {
	m.BlobsProcessed.Inc()
	m.ProcessedBytes.Add(float64(bytes))
}

// RecordBlobFailed increments the failed blob counter.
func (m *Metrics) RecordBlobFailed() {
	m.BlobsFailed.Inc()
}

// RecordLinesWritten adds to the lines written counter.
func (m *Metrics) RecordLinesWritten(count int) {
	m.LinesWritten.Add(float64(count))
}

// SetOutputDirFreeBytes sets the current free space gauge.
func (m *Metrics) SetOutputDirFreeBytes(bytes uint64) {
	m.OutputDirFreeBytes.Set(float64(bytes))
}

// SetActiveParsers sets the current active parsers gauge.
func (m *Metrics) SetActiveParsers(count int) {
	m.ActiveParsers.Set(float64(count))
}

// RecordPoll records a poll attempt and whether it found data.
func (m *Metrics) RecordPoll(foundData bool) {
	m.PollsTotal.Inc()
	if foundData {
		m.PollsWithData.Inc()
	} else {
		m.PollsEmpty.Inc()
	}
}
