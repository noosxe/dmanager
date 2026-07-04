# syntax=docker/dockerfile:1

# Stage 1: Frontend Builder
FROM node:24-alpine AS frontend-builder
WORKDIR /app

# Install pnpm matching the project environment
RUN npm install -g pnpm@11.9.0

# Copy workspace package definitions
COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/

# Install frontend dependencies using build cache for pnpm store
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm --dir frontend install --frozen-lockfile

# Copy frontend source files
COPY frontend/ ./frontend/

# Verify formatting and linting rules
RUN pnpm --dir frontend run lint

# Compile React production assets
RUN pnpm --dir frontend run build


# Stage 2: Backend Builder
FROM golang:1.26-alpine AS backend-builder
WORKDIR /app

# Configure CGO-free build parameters
ENV CGO_ENABLED=0

# Copy Go module manifests
COPY go.mod go.sum ./
RUN go mod download

# Copy built frontend production bundle from Stage 1 for go:embed
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

# Copy backend source files
COPY main.go ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Support cross-compilation target arguments
ARG TARGETOS
ARG TARGETARCH

# Build static backend binary
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o dmanager .


# Stage 3: Runtime Image
FROM alpine:latest

# Install execution dependency tools
RUN apk add --no-cache ca-certificates xz curl

# Accept target architecture from Buildx
ARG TARGETARCH

# Install s6-overlay process supervisor
ARG S6_OVERLAY_VERSION=3.2.0.2
RUN case "${TARGETARCH}" in \
        amd64)   S6_ARCH="x86_64" ;; \
        arm64)   S6_ARCH="aarch64" ;; \
        arm)     S6_ARCH="arm" ;; \
        *)       echo "Unsupported arch: ${TARGETARCH}"; exit 1 ;; \
    esac && \
    curl -sSfL -o /tmp/s6-overlay-noarch.tar.xz "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz" && \
    curl -sSfL -o /tmp/s6-overlay-${S6_ARCH}.tar.xz "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-${S6_ARCH}.tar.xz" && \
    tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz && \
    tar -C / -Jxpf /tmp/s6-overlay-${S6_ARCH}.tar.xz && \
    rm -rf /tmp/s6-overlay-*

# Copy binary to standard path
COPY --from=backend-builder /app/dmanager /usr/local/bin/dmanager

# Create configuration and storage directories
RUN mkdir -p /var/lib/dmanager /etc/dmanager

# Copy s6 process supervision structure
COPY rootfs/ /

# Ensure daemon service script is executable
RUN chmod +x /etc/s6-overlay/s6-rc.d/dmanager/run

# Expose HTTP port
EXPOSE 8080

# Run s6 init supervisor
ENTRYPOINT ["/init"]
