# Build OCI image from NixOS agent configuration
#
# Usage: nix build .#agent-image
#
{
  pkgs,
  config,
  ...
}: let
  # The NixOS system toplevel
  toplevel = config.system.build.toplevel;
in
  pkgs.dockerTools.buildLayeredImage {
    name = "ghcr.io/stanislas/netclode-agent";
    tag = "latest";

    contents = [
      toplevel
      pkgs.bashInteractive
      pkgs.coreutils
      pkgs.findutils
      pkgs.gnugrep
      pkgs.gnutar
      pkgs.gzip
    ];

    config = {
      Entrypoint = ["${toplevel}/init"];
      WorkingDir = "/workspace";

      Env = [
        "PATH=/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin:/usr/bin:/bin"
        "NIX_PATH=nixpkgs=/nix/var/nix/profiles/per-user/root/channels/nixpkgs"
      ];

      Labels = {
        "org.opencontainers.image.title" = "Netclode Agent";
        "org.opencontainers.image.description" = "NixOS-based agent VM for Claude Code sandboxes";
        "org.opencontainers.image.source" = "https://github.com/stanislas/netclode";
      };

      ExposedPorts = {
        "3002/tcp" = {}; # Agent HTTP API
      };
    };

    # Maximize layers for better caching
    maxLayers = 125;
  }
