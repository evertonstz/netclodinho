# Netclode Control Plane — Go service image
#
# Self-contained build: installs buf, copies proto/, and generates Go code inside
# the builder — no CI pre-generation dependency. Uses repo root context (.) like
# the agent Dockerfile.
#
# Layer ordering optimized for cache efficiency:
#   1. Base OS packages + buf (rarely changes)
#   2. Go module download (changes on go.mod/go.sum updates)
#   3. BoxLite native library download (changes on version bumps)
#   4. Proto copy + generate (changes on proto/ changes)
#   5. Source copy + build (changes on every code change)

FROM golang:1.25-bookworm AS builder

WORKDIR /app

ARG BOXLITE_VERSION=v0.8.2
ARG BOXLITE_RELEASE_URL=https://github.com/boxlite-ai/boxlite/releases/download/${BOXLITE_VERSION}
ARG BUF_VERSION=1.51.0

RUN apt-get update && apt-get install -y --no-install-recommends build-essential ca-certificates curl git && rm -rf /var/lib/apt/lists/*

# Install buf (remote plugins only — no local protoc needed)
RUN curl -sSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-x86_64" -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf

# Copy go mod files plus the vendored BoxLite SDK source first so `go mod download`
# can resolve the local replace target.
COPY services/control-plane/go.mod services/control-plane/go.sum ./
COPY services/control-plane/third_party/boxlite-go-sdk ./third_party/boxlite-go-sdk
RUN go mod download

# Download the BoxLite native library and runtime assets at build time.
RUN set -eux; \
    mkdir -p /tmp/boxlite-c /tmp/boxlite-runtime; \
    curl -fsSL "$BOXLITE_RELEASE_URL/boxlite-c-v0.8.2-linux-x64-gnu.tar.gz" -o /tmp/boxlite-c.tgz; \
    tar -xzf /tmp/boxlite-c.tgz -C /tmp/boxlite-c --strip-components=1; \
    cp /tmp/boxlite-c/include/boxlite.h ./third_party/boxlite-go-sdk/boxlite.h; \
    cp /tmp/boxlite-c/lib/libboxlite.a ./third_party/boxlite-go-sdk/libboxlite.a; \
    curl -fsSL "$BOXLITE_RELEASE_URL/boxlite-runtime-v0.8.2-linux-x64-gnu.tar.gz" -o /tmp/boxlite-runtime.tgz; \
    tar -xzf /tmp/boxlite-runtime.tgz -C /tmp/boxlite-runtime --strip-components=1

# Copy proto definitions and generate Go code (self-contained — no CI dependency).
COPY proto/ /proto/
COPY services/control-plane/ ./

RUN buf generate --template buf.gen-docker.yaml /proto

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
