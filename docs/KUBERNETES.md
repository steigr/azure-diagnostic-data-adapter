# Kubernetes Deployment Guide

This guide covers deploying adda as a sidecar container alongside Filebeat in a Kubernetes pod, with Azure Workload Identity for authentication.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Kubernetes Pod                        │
│  ┌─────────────┐    emptyDir     ┌─────────────────┐   │
│  │    adda     │ ──────────────► │    Filebeat     │   │
│  │  (sidecar)  │   /data/output  │                 │   │
│  └─────────────┘                 └─────────────────┘   │
│         │                                │              │
│         ▼                                ▼              │
│  Azure Blob Storage              Elasticsearch/Logstash │
│  (via Workload Identity)                                │
└─────────────────────────────────────────────────────────┘
```

## Prerequisites

- Kubernetes cluster with Azure Workload Identity enabled
- Azure Storage Account with diagnostic data
- Managed Identity with `Storage Blob Data Reader` role on the storage account

## Step 1: Create ServiceAccount with Workload Identity

First, create a ServiceAccount that's federated with an Azure Managed Identity:

```yaml
# serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: adda-workload-identity
  namespace: monitoring
  annotations:
    # Replace with your Azure AD Workload Identity client ID
    azure.workload.identity/client-id: "<AZURE_CLIENT_ID>"
  labels:
    azure.workload.identity/use: "true"
```

### Azure CLI Setup for Workload Identity

```bash
# Set variables
export RESOURCE_GROUP="my-resource-group"
export CLUSTER_NAME="my-aks-cluster"
export LOCATION="eastus"
export IDENTITY_NAME="adda-identity"
export NAMESPACE="monitoring"
export SERVICE_ACCOUNT_NAME="adda-workload-identity"
export STORAGE_ACCOUNT_NAME="mydiagnosticstorage"

# Create Managed Identity
az identity create \
  --name "${IDENTITY_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --location "${LOCATION}"

# Get identity client ID and principal ID
export IDENTITY_CLIENT_ID=$(az identity show \
  --name "${IDENTITY_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --query clientId -o tsv)

export IDENTITY_PRINCIPAL_ID=$(az identity show \
  --name "${IDENTITY_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --query principalId -o tsv)

# Grant Storage Blob Data Reader role
export STORAGE_ACCOUNT_ID=$(az storage account show \
  --name "${STORAGE_ACCOUNT_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --query id -o tsv)

az role assignment create \
  --role "Storage Blob Data Reader" \
  --assignee-object-id "${IDENTITY_PRINCIPAL_ID}" \
  --scope "${STORAGE_ACCOUNT_ID}"

# Get AKS OIDC issuer
export AKS_OIDC_ISSUER=$(az aks show \
  --name "${CLUSTER_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --query oidcIssuerProfile.issuerUrl -o tsv)

# Create federated credential
az identity federated-credential create \
  --name "kubernetes-federated-credential" \
  --identity-name "${IDENTITY_NAME}" \
  --resource-group "${RESOURCE_GROUP}" \
  --issuer "${AKS_OIDC_ISSUER}" \
  --subject "system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT_NAME}"
```

## Step 2: Create ConfigMap for adda

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: adda-config
  namespace: monitoring
data:
  config.yaml: |
    source:
      storage_account_name: "mydiagnosticstorage"
      container_name: "insights-logs-diagnostics"
      file_pattern: ".*\\.json$"

    output:
      directory: "/data/output"
      filename: "diagnostics.ndjson"
      max_size: 50  # MB - rotate at 50MB
      max_backups: 2
      max_age: 1    # days
      gzip: false   # Untested with Filebeat 9.2+ gzip compression

    parsers:
      - id: "ndjson"
        type: "ndjson"
        file_pattern: ".*\\.json$"

    enrichment_template: |
      {
        "_source": {
          "azure": {
            "blob": "{{ .Metadata.BlobName }}",
            "container": "{{ .Metadata.ContainerName }}",
            "storage_account": "{{ .Metadata.StorageAccount }}"
          },
          "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}"
        }
      }

    processing:
      workers: 1
      delete_after_process: true
      backoff_enabled: true
      min_free_space: "192MiB"  # Leave headroom in 256Mi volume
      poll_interval: "1m"
      sort_order: "oldest"

    metrics:
      enabled: true
      address: ":9090"

    logging:
      level: "info"
      format: "json"
```

## Step 3: Create ConfigMap for Filebeat

```yaml
# filebeat-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: filebeat-config
  namespace: monitoring
data:
  filebeat.yml: |
    filebeat.inputs:
      - type: filestream
        id: adda-output
        enabled: true
        paths:
          - /data/output/*.ndjson
        parsers:
          - ndjson:
              keys_under_root: true
              add_error_key: true
              message_key: message

    # Registry settings
    filebeat.registry.flush: 5s

    output.elasticsearch:
      hosts: ["elasticsearch:9200"]
      index: "azure-diagnostics-%{+yyyy.MM.dd}"

    # Or use Logstash
    # output.logstash:
    #   hosts: ["logstash:5044"]

    logging.level: info
    logging.to_stderr: true
```

## Step 4: Deploy Pod with Sidecar

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: adda-filebeat
  namespace: monitoring
  labels:
    app: adda-filebeat
spec:
  replicas: 1
  selector:
    matchLabels:
      app: adda-filebeat
  template:
    metadata:
      labels:
        app: adda-filebeat
        azure.workload.identity/use: "true"
    spec:
      serviceAccountName: adda-workload-identity
      
      # Shared volume for adda output -> filebeat input
      volumes:
        - name: shared-data
          emptyDir:
            medium: Memory    # In-memory for performance
            sizeLimit: 256Mi  # Size limit for the volume
        - name: adda-config
          configMap:
            name: adda-config
        - name: filebeat-config
          configMap:
            name: filebeat-config

      containers:
        # adda sidecar - reads from Azure, writes NDJSON
        - name: adda
          image: ghcr.io/steigr/adda:latest
          args:
            - "--config=/etc/adda/config.yaml"
            - "--min-free-space=192MiB"  # Explicit override
          ports:
            - name: metrics
              containerPort: 9090
              protocol: TCP
          volumeMounts:
            - name: shared-data
              mountPath: /data/output
            - name: adda-config
              mountPath: /etc/adda
              readOnly: true
          resources:
            requests:
              memory: "64Mi"
              cpu: "50m"
            limits:
              memory: "128Mi"
              cpu: "200m"
          livenessProbe:
            httpGet:
              path: /health
              port: metrics
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: metrics
            initialDelaySeconds: 5
            periodSeconds: 10

        # Filebeat - reads NDJSON, ships to Elasticsearch
        - name: filebeat
          image: docker.elastic.co/beats/filebeat:8.11.0
          args:
            - "-c"
            - "/etc/filebeat/filebeat.yml"
            - "-e"
          volumeMounts:
            - name: shared-data
              mountPath: /data/output
              readOnly: true
            - name: filebeat-config
              mountPath: /etc/filebeat
              readOnly: true
          resources:
            requests:
              memory: "64Mi"
              cpu: "50m"
            limits:
              memory: "128Mi"
              cpu: "200m"
```

## Step 5: Apply Resources

```bash
kubectl apply -f serviceaccount.yaml
kubectl apply -f configmap.yaml
kubectl apply -f filebeat-configmap.yaml
kubectl apply -f deployment.yaml
```

## Verification

### Check Pod Status

```bash
kubectl get pods -n monitoring -l app=adda-filebeat
kubectl logs -n monitoring -l app=adda-filebeat -c adda --tail=50
kubectl logs -n monitoring -l app=adda-filebeat -c filebeat --tail=50
```

### Check Metrics

```bash
kubectl port-forward -n monitoring svc/adda-filebeat 9090:9090
curl http://localhost:9090/metrics | grep adda_
```

### Check Health

```bash
kubectl exec -n monitoring deploy/adda-filebeat -c adda -- wget -qO- http://localhost:9090/health
```

## Volume Sizing Guidelines

| Volume Size | Recommended min-free-space | Usable Space |
|-------------|---------------------------|--------------|
| 128Mi       | 96MiB                     | ~32Mi        |
| 256Mi       | 192MiB                    | ~64Mi        |
| 512Mi       | 384MiB                    | ~128Mi       |
| 1Gi         | 768MiB                    | ~256Mi       |

**Rule of thumb**: Set `min-free-space` to 75% of the volume size to ensure:
- Enough buffer for file rotation
- Space for temporary files during processing
- Headroom to prevent disk pressure

## Troubleshooting

### adda not processing blobs

1. Check Workload Identity is configured:
   ```bash
   kubectl describe pod -n monitoring -l app=adda-filebeat | grep -A5 "azure.workload.identity"
   ```

2. Verify Storage Account access:
   ```bash
   kubectl exec -n monitoring deploy/adda-filebeat -c adda -- \
     adda --config=/etc/adda/config.yaml --dry-run --once
   ```

### Disk space issues

1. Check volume usage:
   ```bash
   kubectl exec -n monitoring deploy/adda-filebeat -c adda -- df -h /data/output
   ```

2. Check adda metrics for backoff:
   ```bash
   curl -s http://localhost:9090/metrics | grep adda_backoff
   ```

### Filebeat not picking up files

1. Verify files exist:
   ```bash
   kubectl exec -n monitoring deploy/adda-filebeat -c filebeat -- ls -la /data/output/
   ```

2. Check Filebeat registry:
   ```bash
   kubectl exec -n monitoring deploy/adda-filebeat -c filebeat -- \
     cat /usr/share/filebeat/data/registry/filebeat/log.json
   ```

