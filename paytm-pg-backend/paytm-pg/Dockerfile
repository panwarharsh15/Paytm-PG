# ─── Stage 1: Builder ────────────────────────────────────────────────────────
# Use specific version — no "latest" in production Dockerfiles
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user for the final image
RUN adduser -D -g '' appuser

WORKDIR /build

# Copy go mod files first — Docker layer caching: only re-download deps when go.mod changes
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=0 → static binary (no glibc dependency)
# -ldflags "-w -s" → strip debug symbols (smaller binary)
# -trimpath → remove local file paths from binary (security)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')" \
    -trimpath \
    -o /build/paytm-pg \
    ./cmd/server

# ─── Stage 2: Final image ────────────────────────────────────────────────────
# Distroless is the smallest, most secure base — no shell, no package manager
FROM gcr.io/distroless/static-debian12

# Copy timezone data and certs from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd

# Copy the binary
COPY --from=builder /build/paytm-pg /app/paytm-pg

# Run as non-root — k8s security policy will enforce this too
USER appuser

# Document port (not EXPOSE — that's informational only)
EXPOSE 8080

# Health check — Docker will mark container unhealthy if this fails
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/paytm-pg", "--health-check"] || exit 1

ENTRYPOINT ["/app/paytm-pg"]
