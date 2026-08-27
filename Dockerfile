# syntax=docker/dockerfile:1

# Stage 1: Frontend Builder
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder
WORKDIR /app

# Install pnpm matching the project environment
RUN npm install -g pnpm@11.9.0

# Copy workspace package definitions
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./frontend/

# Install frontend dependencies using build cache for pnpm store
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm --dir frontend install --frozen-lockfile

# Copy frontend source files
COPY frontend/ ./frontend/

# Verify formatting and linting rules
RUN pnpm --dir frontend run lint

# Compile React production assets
RUN pnpm --dir frontend run build


# Stage 2: Backend Builder
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend-builder
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

# Build metadata injected into the binary (override with --build-arg)
ARG VERSION=dev
ARG COMMIT=none

# Build static backend binary with version metadata baked in
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w \
      -X dmanager/internal/version.Version=${VERSION} \
      -X dmanager/internal/version.Commit=${COMMIT} \
      -X dmanager/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o dmanager .


# Stage 3: Downloader (runs natively on build host, no QEMU)
FROM --platform=$BUILDPLATFORM alpine:latest AS downloader

# Install tools needed for downloading and extracting
RUN apk add --no-cache ca-certificates xz curl

# Accept target architecture from Buildx
ARG TARGETARCH
ARG S6_OVERLAY_VERSION=3.2.0.2
# SHA-256 digests of the release tarballs, cross-checked against the .sha256
# sidecar files published at:
# https://github.com/just-containers/s6-overlay/releases/tag/v${S6_OVERLAY_VERSION}
ARG S6_NOARCH_SHA256=6dbcde158a3e78b9bb141d7bcb5ccb421e563523babbe2c64470e76f4fd02dae
ARG S6_AMD64_SHA256=59289456ab1761e277bd456a95e737c06b03ede99158beb24f12b165a904f478
ARG S6_ARM64_SHA256=8b22a2eaca4bf0b27a43d36e65c89d2701738f628d1abd0cea5569619f66f785
ARG S6_ARM_SHA256=e00b0d94f2cf1e4178c922c7b90181a619981103be635fe5fb0c9547e4193c52

# Download and extract s6-overlay for the target arch into a temporary directory
RUN mkdir -p /tmp/s6-root && \
    case "${TARGETARCH}" in \
        amd64)   S6_ARCH="x86_64";  S6_SHA256="${S6_AMD64_SHA256}" ;; \
        arm64)   S6_ARCH="aarch64"; S6_SHA256="${S6_ARM64_SHA256}" ;; \
        arm)     S6_ARCH="arm";     S6_SHA256="${S6_ARM_SHA256}" ;; \
        *)       echo "Unsupported arch: ${TARGETARCH}"; exit 1 ;; \
    esac && \
    curl -sSfL -o /tmp/s6-overlay-noarch.tar.xz "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz" && \
    curl -sSfL -o /tmp/s6-overlay-${S6_ARCH}.tar.xz "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-${S6_ARCH}.tar.xz" && \
    echo "${S6_NOARCH_SHA256}  /tmp/s6-overlay-noarch.tar.xz" | sha256sum -c - && \
    echo "${S6_SHA256}  /tmp/s6-overlay-${S6_ARCH}.tar.xz" | sha256sum -c - && \
    tar -C /tmp/s6-root -Jxpf /tmp/s6-overlay-noarch.tar.xz && \
    tar -C /tmp/s6-root -Jxpf /tmp/s6-overlay-${S6_ARCH}.tar.xz

# Create empty data and configuration directories to copy into runtime image
RUN mkdir -p /tmp/var-lib-dmanager /tmp/etc-dmanager


# Stage 4: Runtime Image (no RUN instructions, zero QEMU overhead!)
FROM alpine:latest

# Copy certificates from downloader stage
COPY --from=downloader /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy empty data and configuration directories
COPY --from=downloader /tmp/var-lib-dmanager /var/lib/dmanager
COPY --from=downloader /tmp/etc-dmanager /etc/dmanager

# Copy s6-overlay files
COPY --from=downloader /tmp/s6-root/ /

# Copy statically compiled binary
COPY --from=backend-builder /app/dmanager /usr/local/bin/dmanager

# Copy s6 process supervision structure (permissions preserved from Git/host)
COPY rootfs/ /

# Expose HTTP port
EXPOSE 9283

# Run s6 init supervisor
ENTRYPOINT ["/init"]
