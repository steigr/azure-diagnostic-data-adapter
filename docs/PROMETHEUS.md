# Prometheus Integration Guide

This guide covers integrating adda with Prometheus for monitoring, alerting, and visualization with Grafana.

## Metrics Overview

adda exposes Prometheus metrics at the `/metrics` endpoint (default port: 9090).

### Available Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `adda_blobs_processed_total` | Counter | Total number of successfully processed blobs |
| `adda_blobs_failed_total` | Counter | Total number of failed blob processing attempts |
| `adda_processed_bytes_total` | Counter | Total size of processed data in bytes |
| `adda_lines_written_total` | Counter | Total number of lines written to output files |
| `adda_blob_processing_seconds` | Histogram | Time taken for processing each blob |
| `adda_output_dir_free_bytes` | Gauge | Current free space in output directory |
| `adda_active_parsers` | Gauge | Number of currently active parsers |
| `adda_polls_total` | Counter | Total poll attempts to Azure Storage |
| `adda_polls_with_data_total` | Counter | Polls that found data to process |
| `adda_polls_empty_total` | Counter | Polls with no data to process |
| `adda_backoff_total` | Counter | Backoff events due to low disk space |
| `adda_backoff_active` | Gauge | Whether backoff is active (1) or not (0) |

## Kubernetes Service and ServiceMonitor

### Step 1: Create Service

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: adda-metrics
  namespace: monitoring
  labels:
    app: adda
    app.kubernetes.io/name: adda
    app.kubernetes.io/component: metrics
spec:
  selector:
    app: adda-filebeat
  ports:
    - name: metrics
      port: 9090
      targetPort: 9090
      protocol: TCP
  type: ClusterIP
```

### Step 2: Create ServiceMonitor (for Prometheus Operator)

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: adda
  namespace: monitoring
  labels:
    app: adda
    release: prometheus  # Match your Prometheus Operator release label
spec:
  selector:
    matchLabels:
      app: adda
  namespaceSelector:
    matchNames:
      - monitoring
  endpoints:
    - port: metrics
      interval: 30s
      scrapeTimeout: 10s
      path: /metrics
      scheme: http
```

### Step 3: Apply Resources

```bash
kubectl apply -f service.yaml
kubectl apply -f servicemonitor.yaml
```

## Prometheus Alerting Rules

Create PrometheusRule for alerting on critical conditions:

```yaml
# prometheusrule.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adda-alerts
  namespace: monitoring
  labels:
    app: adda
    release: prometheus
spec:
  groups:
    - name: adda.rules
      interval: 30s
      rules:
        # ============================================
        # Processing Errors
        # ============================================
        - alert: AddaBlobProcessingErrors
          expr: |
            rate(adda_blobs_failed_total[5m]) > 0
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "adda is experiencing blob processing errors"
            description: |
              adda instance {{ $labels.instance }} has been failing to process blobs
              for the last 5 minutes. Current failure rate: {{ $value | printf "%.2f" }}/s

        - alert: AddaHighErrorRate
          expr: |
            (
              rate(adda_blobs_failed_total[5m]) /
              (rate(adda_blobs_processed_total[5m]) + rate(adda_blobs_failed_total[5m]))
            ) > 0.1
          for: 10m
          labels:
            severity: critical
          annotations:
            summary: "adda has high error rate (>10%)"
            description: |
              adda instance {{ $labels.instance }} has an error rate of 
              {{ $value | printf "%.1f" }}% over the last 10 minutes.

        # ============================================
        # Disk Space Issues
        # ============================================
        - alert: AddaDiskSpaceBackoff
          expr: |
            adda_backoff_active == 1
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "adda is backing off due to low disk space"
            description: |
              adda instance {{ $labels.instance }} has been in disk space backoff mode
              for 5 minutes. Free space: {{ with query "adda_output_dir_free_bytes" }}{{ . | first | value | humanize1024 }}{{ end }}

        - alert: AddaDiskSpaceCritical
          expr: |
            adda_output_dir_free_bytes < 50 * 1024 * 1024
          for: 2m
          labels:
            severity: critical
          annotations:
            summary: "adda output directory has less than 50MB free"
            description: |
              adda instance {{ $labels.instance }} has only 
              {{ $value | humanize1024 }} free in the output directory.

        - alert: AddaFrequentBackoffs
          expr: |
            increase(adda_backoff_total[1h]) > 10
          for: 0m
          labels:
            severity: warning
          annotations:
            summary: "adda is experiencing frequent disk space backoffs"
            description: |
              adda instance {{ $labels.instance }} has triggered {{ $value }} backoffs
              in the last hour. Consider increasing volume size or reducing data throughput.

        # ============================================
        # Azure API Polling Issues
        # ============================================
        - alert: AddaHighPollingRate
          expr: |
            rate(adda_polls_total[5m]) > 0.5
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "adda is polling Azure Storage too frequently"
            description: |
              adda instance {{ $labels.instance }} is polling at 
              {{ $value | printf "%.2f" }} requests/second. Consider increasing poll_interval.

        - alert: AddaNoDataPolls
          expr: |
            (
              rate(adda_polls_empty_total[30m]) /
              rate(adda_polls_total[30m])
            ) > 0.95
          for: 1h
          labels:
            severity: info
          annotations:
            summary: "adda is mostly polling without finding data"
            description: |
              adda instance {{ $labels.instance }} has found no data in 
              {{ $value | printf "%.0f" }}% of polls over the last hour.
              This may indicate no new diagnostic data or misconfigured file patterns.

        - alert: AddaPollingErrors
          expr: |
            rate(adda_polls_total[5m]) == 0 and up{job="adda"} == 1
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "adda has stopped polling Azure Storage"
            description: |
              adda instance {{ $labels.instance }} appears to have stopped polling.
              Check for connectivity issues or application errors.

        # ============================================
        # Processing Performance
        # ============================================
        - alert: AddaSlowProcessing
          expr: |
            histogram_quantile(0.95, rate(adda_blob_processing_seconds_bucket[10m])) > 30
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: "adda blob processing is slow"
            description: |
              adda instance {{ $labels.instance }} p95 processing time is 
              {{ $value | printf "%.1f" }}s. Check for large blobs or parser issues.

        # ============================================
        # Availability
        # ============================================
        - alert: AddaDown
          expr: |
            up{job="adda"} == 0
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "adda is down"
            description: |
              adda instance {{ $labels.instance }} has been unreachable for 5 minutes.
```

### Apply Alerting Rules

```bash
kubectl apply -f prometheusrule.yaml
```

## Grafana Dashboard

Import the following dashboard JSON into Grafana:

```json
{
  "annotations": {
    "list": []
  },
  "description": "Azure Diagnostic Data Adapter (adda) Monitoring Dashboard",
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "collapsed": false,
      "gridPos": { "h": 1, "w": 24, "x": 0, "y": 0 },
      "id": 1,
      "title": "Overview",
      "type": "row"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "green", "value": null }
            ]
          },
          "unit": "short"
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 0, "y": 1 },
      "id": 2,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "targets": [
        {
          "expr": "sum(increase(adda_blobs_processed_total[24h]))",
          "legendFormat": "Blobs Processed (24h)"
        }
      ],
      "title": "Blobs Processed (24h)",
      "type": "stat"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "thresholds" },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "green", "value": null },
              { "color": "yellow", "value": 1 },
              { "color": "red", "value": 10 }
            ]
          },
          "unit": "short"
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 4, "y": 1 },
      "id": 3,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "targets": [
        {
          "expr": "sum(increase(adda_blobs_failed_total[24h]))",
          "legendFormat": "Failed"
        }
      ],
      "title": "Failed Blobs (24h)",
      "type": "stat"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "thresholds" },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "red", "value": null },
              { "color": "yellow", "value": 52428800 },
              { "color": "green", "value": 104857600 }
            ]
          },
          "unit": "bytes"
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 8, "y": 1 },
      "id": 4,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "targets": [
        {
          "expr": "adda_output_dir_free_bytes",
          "legendFormat": "Free Space"
        }
      ],
      "title": "Output Dir Free Space",
      "type": "stat"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "thresholds" },
          "mappings": [
            { "options": { "0": { "text": "OK" } }, "type": "value" },
            { "options": { "1": { "text": "BACKOFF" } }, "type": "value" }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              { "color": "green", "value": null },
              { "color": "red", "value": 1 }
            ]
          }
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 12, "y": 1 },
      "id": 5,
      "options": {
        "colorMode": "background",
        "graphMode": "none",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "targets": [
        {
          "expr": "adda_backoff_active",
          "legendFormat": "Backoff Status"
        }
      ],
      "title": "Disk Space Status",
      "type": "stat"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "unit": "bytes"
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 16, "y": 1 },
      "id": 6,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "targets": [
        {
          "expr": "sum(increase(adda_processed_bytes_total[24h]))",
          "legendFormat": "Bytes Processed"
        }
      ],
      "title": "Data Processed (24h)",
      "type": "stat"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "unit": "s"
        }
      },
      "gridPos": { "h": 4, "w": 4, "x": 20, "y": 1 },
      "id": 7,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        }
      },
      "targets": [
        {
          "expr": "histogram_quantile(0.95, rate(adda_blob_processing_seconds_bucket[5m]))",
          "legendFormat": "p95 Processing Time"
        }
      ],
      "title": "Processing Time (p95)",
      "type": "stat"
    },
    {
      "collapsed": false,
      "gridPos": { "h": 1, "w": 24, "x": 0, "y": 5 },
      "id": 10,
      "title": "Processing",
      "type": "row"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisCenteredZero": false,
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "pointSize": 5,
            "showPoints": "never",
            "stacking": { "mode": "none" }
          },
          "unit": "short"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 6 },
      "id": 11,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "rate(adda_blobs_processed_total[5m]) * 60",
          "legendFormat": "Processed/min"
        },
        {
          "expr": "rate(adda_blobs_failed_total[5m]) * 60",
          "legendFormat": "Failed/min"
        }
      ],
      "title": "Blob Processing Rate",
      "type": "timeseries"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "axisCenteredZero": false,
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "pointSize": 5,
            "showPoints": "never"
          },
          "unit": "bytes"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 6 },
      "id": 12,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "adda_output_dir_free_bytes",
          "legendFormat": "Free Space"
        }
      ],
      "title": "Output Directory Free Space",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": { "h": 1, "w": 24, "x": 0, "y": 14 },
      "id": 20,
      "title": "Polling & Azure API",
      "type": "row"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "showPoints": "never",
            "stacking": { "mode": "normal" }
          },
          "unit": "short"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 15 },
      "id": 21,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "rate(adda_polls_with_data_total[5m]) * 60",
          "legendFormat": "With Data"
        },
        {
          "expr": "rate(adda_polls_empty_total[5m]) * 60",
          "legendFormat": "Empty"
        }
      ],
      "title": "Poll Results (per minute)",
      "type": "timeseries"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "showPoints": "never"
          },
          "unit": "short"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 15 },
      "id": 22,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "increase(adda_backoff_total[1h])",
          "legendFormat": "Backoffs (1h)"
        }
      ],
      "title": "Disk Space Backoffs",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": { "h": 1, "w": 24, "x": 0, "y": 23 },
      "id": 30,
      "title": "Performance",
      "type": "row"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "showPoints": "never"
          },
          "unit": "s"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 24 },
      "id": 31,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "histogram_quantile(0.50, rate(adda_blob_processing_seconds_bucket[5m]))",
          "legendFormat": "p50"
        },
        {
          "expr": "histogram_quantile(0.90, rate(adda_blob_processing_seconds_bucket[5m]))",
          "legendFormat": "p90"
        },
        {
          "expr": "histogram_quantile(0.99, rate(adda_blob_processing_seconds_bucket[5m]))",
          "legendFormat": "p99"
        }
      ],
      "title": "Blob Processing Time",
      "type": "timeseries"
    },
    {
      "datasource": { "type": "prometheus", "uid": "${datasource}" },
      "fieldConfig": {
        "defaults": {
          "color": { "mode": "palette-classic" },
          "custom": {
            "drawStyle": "line",
            "fillOpacity": 10,
            "lineWidth": 1,
            "showPoints": "never"
          },
          "unit": "binBps"
        }
      },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 24 },
      "id": 32,
      "options": {
        "legend": { "displayMode": "list", "placement": "bottom" },
        "tooltip": { "mode": "multi" }
      },
      "targets": [
        {
          "expr": "rate(adda_processed_bytes_total[5m])",
          "legendFormat": "Throughput"
        }
      ],
      "title": "Data Throughput",
      "type": "timeseries"
    }
  ],
  "refresh": "30s",
  "schemaVersion": 38,
  "tags": ["adda", "azure", "diagnostics"],
  "templating": {
    "list": [
      {
        "current": {},
        "hide": 0,
        "includeAll": false,
        "label": "Datasource",
        "name": "datasource",
        "options": [],
        "query": "prometheus",
        "refresh": 1,
        "type": "datasource"
      }
    ]
  },
  "time": { "from": "now-6h", "to": "now" },
  "timepicker": {},
  "timezone": "browser",
  "title": "adda - Azure Diagnostic Data Adapter",
  "uid": "adda-dashboard",
  "version": 1
}
```

### Import Dashboard

1. Open Grafana
2. Go to **Dashboards** → **Import**
3. Paste the JSON above or upload as a file
4. Select your Prometheus datasource
5. Click **Import**

## Quick Reference

### Verify Metrics Endpoint

```bash
# Port-forward to adda
kubectl port-forward -n monitoring svc/adda-metrics 9090:9090

# Check metrics
curl -s http://localhost:9090/metrics | grep -E "^adda_"
```

### Key Metrics to Watch

| Metric | Good Value | Warning Threshold |
|--------|------------|-------------------|
| `adda_backoff_active` | 0 | 1 (disk space issue) |
| `adda_blobs_failed_total` rate | 0 | Any increase |
| `adda_output_dir_free_bytes` | >100MB | <50MB |
| `adda_polls_total` rate | <0.5/s | >1/s (too frequent) |

### Common PromQL Queries

```promql
# Success rate (last hour)
sum(rate(adda_blobs_processed_total[1h])) / 
(sum(rate(adda_blobs_processed_total[1h])) + sum(rate(adda_blobs_failed_total[1h])))

# Average processing time
rate(adda_blob_processing_seconds_sum[5m]) / rate(adda_blob_processing_seconds_count[5m])

# Polls with data ratio
sum(rate(adda_polls_with_data_total[1h])) / sum(rate(adda_polls_total[1h]))

# Bytes processed per hour
sum(increase(adda_processed_bytes_total[1h]))
```

