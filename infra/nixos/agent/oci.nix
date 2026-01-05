# Build OCI image from NixOS agent configuration
#
# Usage: nix build .#agent-image
#
# Note: Kata Containers mounts the container rootfs read-only via virtio-fs.
# We use a custom init wrapper that sets up tmpfs overlays before NixOS boots.
#
{
  pkgs,
  config,
  ...
}: let
  # The NixOS system toplevel
  toplevel = config.system.build.toplevel;

  # Init wrapper script that sets up tmpfs overlays for writable paths
  # This runs before NixOS init to handle Kata's read-only virtio-fs rootfs
  initWrapper = pkgs.writeShellScript "init-wrapper" ''
    #!/bin/sh
    set -e

    echo "Setting up tmpfs overlays for Kata read-only rootfs..."

    # Mount essential filesystems first
    mount -t proc proc /proc 2>/dev/null || true
    mount -t sysfs sys /sys 2>/dev/null || true
    mount -t devtmpfs dev /dev 2>/dev/null || true

    # Create tmpfs for writable areas
    mount -t tmpfs -o mode=755,size=512M tmpfs /run
    mount -t tmpfs -o mode=1777,size=256M tmpfs /tmp
    mount -t tmpfs -o mode=755,size=256M tmpfs /var

    # Set up /etc overlay: tmpfs upper + image etc as lower
    mkdir -p /run/etc-upper /run/etc-work
    if [ -d /etc ]; then
      mount -t overlay overlay -o lowerdir=/etc,upperdir=/run/etc-upper,workdir=/run/etc-work /etc 2>/dev/null || {
        # If overlay fails, use tmpfs and copy
        echo "Overlay mount failed, using tmpfs copy..."
        cp -a /etc /run/etc-copy 2>/dev/null || true
        mount -t tmpfs tmpfs /etc
        cp -a /run/etc-copy/* /etc/ 2>/dev/null || true
      }
    fi

    # Create required directories
    mkdir -p /var/log /var/run /var/tmp /var/lib
    mkdir -p /run/systemd /run/user

    echo "Tmpfs overlays ready, starting NixOS init..."

    # Exec to NixOS init
    exec ${toplevel}/init "$@"
  '';
in
  pkgs.dockerTools.buildLayeredImage {
    name = "ghcr.io/angristan/netclode-agent";
    tag = "latest";

    contents = [
      toplevel
      pkgs.bashInteractive
      pkgs.coreutils
      pkgs.findutils
      pkgs.gnugrep
      pkgs.gnutar
      pkgs.gzip
      pkgs.util-linux  # for mount
    ];

    config = {
      Entrypoint = ["${initWrapper}"];
      WorkingDir = "/workspace";

      Env = [
        "PATH=/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:/usr/bin:/bin"
        "NIX_PATH=nixpkgs=/nix/var/nix/profiles/per-user/root/channels/nixpkgs"
      ];

      Labels = {
        "org.opencontainers.image.title" = "Netclode Agent";
        "org.opencontainers.image.description" = "NixOS-based agent VM for Claude Code sandboxes";
        "org.opencontainers.image.source" = "https://github.com/angristan/netclode";
      };

      ExposedPorts = {
        "3002/tcp" = {}; # Agent HTTP API
      };
    };

    # Maximize layers for better caching
    maxLayers = 125;
  }
