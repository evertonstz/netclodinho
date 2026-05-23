FROM golang:1.25-bookworm AS builder

WORKDIR /app

ARG BOXLITE_VERSION=v0.8.2
ARG BOXLITE_RELEASE_URL=https://github.com/boxlite-ai/boxlite/releases/download/${BOXLITE_VERSION}

RUN apt-get update && apt-get install -y --no-install-recommends build-essential ca-certificates curl git && rm -rf /var/lib/apt/lists/*

# Install buf for proto code generation
ARG BUF_VERSION=1.51.0
RUN curl -sSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-x86_64" -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Copy go mod files plus the vendored BoxLite SDK source first so `go mod download`
# can resolve the local replace target. Paths are relative to repo root (monorepo context).
COPY services/control-plane/go.mod services/control-plane/go.sum ./
COPY services/control-plane/third_party/boxlite-go-sdk ./third_party/boxlite-go-sdk
RUN go mod download

# Download the BoxLite native library and runtime assets at build time.
RUN set -eux; \
    mkdir -p /tmp/boxlite-runtime; \
    ARCH=$(if [ "$(uname -m)" = "aarch64" ]; then echo "arm64"; else echo "x86_64"; fi); \
    if [ "$ARCH" = "arm64" ]; then \
      curl -fsSL "${BOXLITE_RELEASE_URL}/boxlite-arm64-runtime.tgz" -o /tmp/boxlite-runtime.tgz; \
    else \
      curl -fsSL "${BOXLITE_RELEASE_URL}/boxlite-x86_64-runtime.tgz" -o /tmp/boxlite-runtime.tgz; \
    fi; \
    tar -xzf /tmp/boxlite-runtime.tgz -C /tmp/boxlite-runtime --strip-components=1

# Copy the control-plane source.
COPY services/control-plane/ .

# Generate Go proto code (gen/ is gitignored, proto/ is at repo root).
RUN buf generate --template /proto/buf.gen.yaml --path /proto/netclode/v1

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o control-plane ./cmd/control-plane

# Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 libgcc-s1 e2fsprogs bash curl && rm -rf /var/lib/apt/lists/*

COPY --from=builder /tmp/boxlite-runtime/ /runtime/
COPY --from=builder /app/control-plane /control-plane

RUN mkdir -p /var/lib/boxlite \
    && chown -R 65534:65534 /var/lib/boxlite

USER 65534:65534
ENV LD_LIBRARY_PATH=/runtime

EXPOSE 3000
ENTRYPOINT ["/control-plane"]
