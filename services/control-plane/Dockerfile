FROM golang:1.25-bookworm AS builder

WORKDIR /app

ARG BOXLITE_VERSION=v0.8.2
ARG BOXLITE_RELEASE_URL=https://github.com/boxlite-ai/boxlite/releases/download/${BOXLITE_VERSION}

RUN apt-get update && apt-get install -y --no-install-recommends build-essential ca-certificates curl git && rm -rf /var/lib/apt/lists/*

# Copy go mod files plus the vendored BoxLite SDK source first so `go mod download`
# can resolve the local replace target.
COPY go.mod go.sum ./
COPY third_party/boxlite-go-sdk ./third_party/boxlite-go-sdk
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Download the BoxLite native library and runtime assets at build time.
RUN set -eux; \
    mkdir -p /tmp/boxlite-c /tmp/boxlite-runtime; \
    curl -fsSL "$BOXLITE_RELEASE_URL/boxlite-c-v0.8.2-linux-x64-gnu.tar.gz" -o /tmp/boxlite-c.tgz; \
    tar -xzf /tmp/boxlite-c.tgz -C /tmp/boxlite-c --strip-components=1; \
    cp /tmp/boxlite-c/lib/libboxlite.a ./third_party/boxlite-go-sdk/libboxlite.a; \
    curl -fsSL "$BOXLITE_RELEASE_URL/boxlite-runtime-v0.8.2-linux-x64-gnu.tar.gz" -o /tmp/boxlite-runtime.tgz; \
    tar -xzf /tmp/boxlite-runtime.tgz -C /tmp/boxlite-runtime --strip-components=1

# Copy the rest of the source and build.
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o control-plane ./cmd/control-plane

# Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 libgcc-s1 e2fsprogs && rm -rf /var/lib/apt/lists/*

COPY --from=builder /tmp/boxlite-runtime/ /runtime/
COPY --from=builder /app/control-plane /control-plane

# Create runtime directories owned by non-root user
RUN mkdir -p /var/lib/netclode/workspaces /var/lib/boxlite \
    && chown -R 65534:65534 /var/lib/netclode /var/lib/boxlite

# Run as non-root user
USER 65534:65534
ENV LD_LIBRARY_PATH=/runtime

EXPOSE 3000

ENTRYPOINT ["/control-plane"]
