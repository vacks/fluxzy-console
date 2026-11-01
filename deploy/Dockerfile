# syntax=docker/dockerfile:1.7
# Single fluxzy-console image. The console supervises one bundled CLI: the
# console fork built from fork/fluxzy. The official fluxzy image and official
# CLI are intentionally absent so body transforms and hot rule reload cannot be
# silently bypassed by a runtime update.

ARG GO_VERSION=1.27
ARG NODE_VERSION=22
ARG CONSOLE_VERSION=1.0.0
ARG FLUXZY_CLI_RELEASE=1.39.52.38106
ARG ALPINE_VERSION=3.24

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
WORKDIR /app
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS gobuild
ARG TARGETARCH
ARG CONSOLE_VERSION
ARG FLUXZY_CLI_RELEASE
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /app/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w \
      -X github.com/vacks/fluxzy-console/internal/version.Version=${CONSOLE_VERSION} \
      -X github.com/vacks/fluxzy-console/internal/version.BundledFluxzyRelease=${FLUXZY_CLI_RELEASE}" \
    -o /out/console ./cmd/console

# Production contains only the console fork CLI. Native packages mirror what
# the self-contained musl fluxzy binary needs (TLS, pcap, compression).
FROM --platform=linux/amd64 alpine:${ALPINE_VERSION} AS prod-amd64
ARG CONSOLE_VERSION
ARG FLUXZY_CLI_RELEASE
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
LABEL org.opencontainers.image.title="fluxzy-console" \
      org.opencontainers.image.version="${CONSOLE_VERSION}" \
      org.opencontainers.image.source="https://github.com/vacks/fluxzy-console" \
      org.opencontainers.image.description="Self-hosted fluxzy traffic audit console (bundled CLI ${FLUXZY_CLI_RELEASE})"
RUN apk add --no-cache \
      ca-certificates tini \
      gcompat libgcc libstdc++ libssl3 libpcap zlib ncurses-libs
WORKDIR /app
COPY --from=gobuild /out/console /app/console
COPY fork/fluxzy/out/linux-amd64/fluxzy /opt/fluxzy/fork/fluxzy
COPY fork/fluxzy/out/linux-amd64/fluxzy-fork.json /opt/fluxzy/fork/fork.json
COPY fork/fluxzy/out/linux-amd64/fluxzy.sha256 /opt/fluxzy/fork/fluxzy.sha256
COPY NOTICE.md /app/NOTICE.md
COPY fork/fluxzy/UPSTREAM-LICENSE.md /app/UPSTREAM-LICENSE.md
ENV HOME=/var/lib/fluxzy-console/fluxzy-home \
    TMPDIR=/var/lib/fluxzy-console/tmp \
    DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
EXPOSE 443 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/v1/healthz >/dev/null || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/app/console"]

# Development target for Apple Silicon (glibc arm64 fork build).
FROM --platform=linux/arm64 debian:bookworm-slim AS dev-arm64
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates libssl3 libpcap0.8 libgcc-s1 libstdc++6 libncurses6 zlib1g wget tini \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=gobuild /out/console /app/console
COPY fork/fluxzy/out/linux-arm64/fluxzy /opt/fluxzy/fork/fluxzy
COPY fork/fluxzy/out/linux-arm64/fluxzy-fork.json /opt/fluxzy/fork/fork.json
COPY fork/fluxzy/out/linux-arm64/fluxzy.sha256 /opt/fluxzy/fork/fluxzy.sha256
COPY NOTICE.md /app/NOTICE.md
COPY fork/fluxzy/UPSTREAM-LICENSE.md /app/UPSTREAM-LICENSE.md
ENV HOME=/var/lib/fluxzy-console/fluxzy-home \
    TMPDIR=/var/lib/fluxzy-console/tmp \
    DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
EXPOSE 443 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/v1/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/app/console"]
