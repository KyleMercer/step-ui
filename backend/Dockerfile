FROM golang:1.21-bullseye AS builder

# Pin the Smallstep CLI to a specific, checksummed release instead of
# "latest" -- unpinned "latest" downloads aren't reproducible (a rebuild next
# month can silently pick up a different CLI version) and had no integrity
# check at all. Bump STEP_CLI_VERSION here when you want to update it.
ARG STEP_CLI_VERSION=0.30.6
# Populated automatically by Docker/buildx for the platform being built
# (amd64, arm64, ...) -- do not set this by hand.
ARG TARGETARCH

# Install step CLI and build dependencies
RUN apt-get update && apt-get install -y \
    curl \
    gcc \
    sqlite3 \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Download the step CLI for the architecture we're actually building for
# (was hardcoded to amd64 -- broken on arm64 hosts like a Raspberry Pi),
# and verify it against Smallstep's published checksums before trusting it.
RUN set -eux; \
    cd /tmp; \
    curl -fLO "https://github.com/smallstep/cli/releases/download/v${STEP_CLI_VERSION}/step_linux_${TARGETARCH}.tar.gz"; \
    curl -fLO "https://github.com/smallstep/cli/releases/download/v${STEP_CLI_VERSION}/checksums.txt"; \
    grep "step_linux_${TARGETARCH}.tar.gz" checksums.txt | sha256sum -c -; \
    tar xz -C /tmp -f "step_linux_${TARGETARCH}.tar.gz"; \
    cp "/tmp/step_linux_${TARGETARCH}/bin/step" /usr/local/bin/step; \
    chmod +x /usr/local/bin/step; \
    rm -rf /tmp/step_linux_${TARGETARCH}* /tmp/checksums.txt

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod ./
COPY go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Tidy modules and build
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main ./cmd

# Final stage
FROM debian:bullseye-slim

ARG STEP_CLI_VERSION=0.30.6
ARG TARGETARCH

# Install step CLI and openssl
RUN apt-get update && apt-get install -y \
    curl \
    openssl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Same pinned + checksummed step CLI download as the builder stage
RUN set -eux; \
    cd /tmp; \
    curl -fLO "https://github.com/smallstep/cli/releases/download/v${STEP_CLI_VERSION}/step_linux_${TARGETARCH}.tar.gz"; \
    curl -fLO "https://github.com/smallstep/cli/releases/download/v${STEP_CLI_VERSION}/checksums.txt"; \
    grep "step_linux_${TARGETARCH}.tar.gz" checksums.txt | sha256sum -c -; \
    tar xz -C /tmp -f "step_linux_${TARGETARCH}.tar.gz"; \
    cp "/tmp/step_linux_${TARGETARCH}/bin/step" /usr/local/bin/step; \
    chmod +x /usr/local/bin/step; \
    rm -rf /tmp/step_linux_${TARGETARCH}* /tmp/checksums.txt

# Create app user
RUN useradd -m -s /bin/bash appuser

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/main .

# Create data directory
RUN mkdir -p /app/data && chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Run the application
CMD ["./main"]
