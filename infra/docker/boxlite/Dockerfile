FROM ubuntu:22.04

ARG BOXLITE_VERSION=0.8.2
ARG TARGETARCH

# Install runtime dependencies for Boxlite (KVM, bwrap, OCI tools)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    bubblewrap \
    qemu-utils \
    && rm -rf /var/lib/apt/lists/*

# Download pre-built boxlite CLI binary for the target architecture
RUN case "${TARGETARCH:-amd64}" in \
      "amd64") ARCH="x86_64-unknown-linux-gnu" ;; \
      "arm64") ARCH="aarch64-unknown-linux-gnu" ;; \
      *) echo "Unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -fsSL \
      "https://github.com/boxlite-ai/boxlite/releases/download/v${BOXLITE_VERSION}/boxlite-cli-v${BOXLITE_VERSION}-${ARCH}.tar.gz" \
      | tar xz -C /usr/local/bin && \
    chmod +x /usr/local/bin/boxlite

# Verify installation
RUN boxlite --version

EXPOSE 8100

ENTRYPOINT ["boxlite"]
CMD ["serve", "--host", "0.0.0.0", "--port", "8100"]
