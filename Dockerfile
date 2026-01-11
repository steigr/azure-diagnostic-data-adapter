# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.version=${VERSION}" \
    -o /app/bin/adda ./cmd/adda

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S adda && \
    adduser -u 1000 -S adda -G adda

# Create directories
RUN mkdir -p /etc/adda /var/log/adda && \
    chown -R adda:adda /etc/adda /var/log/adda

# Copy binary from builder
COPY --from=builder /app/bin/adda /usr/local/bin/adda

# Copy example config
COPY --from=builder /app/examples/config.yaml /etc/adda/config.yaml.example

# Switch to non-root user
USER adda

# Set working directory
WORKDIR /var/log/adda

# Expose metrics port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/health || exit 1

# Default command
ENTRYPOINT ["adda"]
CMD ["--config", "/etc/adda/config.yaml"]
