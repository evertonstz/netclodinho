# Tailscale configuration
#
# To auto-authenticate, place authkey at /var/secrets/tailscale-authkey
#
{
  config,
  lib,
  pkgs,
  ...
}: {
  # Enable Tailscale
  services.tailscale = {
    enable = true;
    useRoutingFeatures = "server";
  };

  # Open firewall for Tailscale
  networking.firewall = {
    trustedInterfaces = ["tailscale0"];
    allowedUDPPorts = [config.services.tailscale.port];
  };

  # Auto-connect service
  systemd.services.tailscale-autoconnect = {
    description = "Automatic Tailscale connection";
    after = ["tailscaled.service"];
    requires = ["tailscaled.service"];
    wantedBy = ["multi-user.target"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      set -euo pipefail

      # Wait for tailscaled
      sleep 2

      # Check if already authenticated
      status="$(${pkgs.tailscale}/bin/tailscale status --json 2>/dev/null | ${pkgs.jq}/bin/jq -r '.BackendState // "NoState"')"
      if [ "$status" = "Running" ]; then
        echo "Tailscale already connected"
        exit 0
      fi

      # Look for authkey
      if [ -f /var/secrets/tailscale-authkey ]; then
        authkey="$(cat /var/secrets/tailscale-authkey)"
        echo "Connecting to Tailscale..."
        ${pkgs.tailscale}/bin/tailscale up --auth-key="$authkey" --ssh
        rm -f /var/secrets/tailscale-authkey
        echo "Tailscale connected!"
      else
        echo "No authkey found. Authenticate manually: tailscale up --ssh"
      fi
    '';
  };

  # Tailscale serve for control plane
  systemd.services.tailscale-serve = {
    description = "Tailscale Serve for Control Plane";
    after = ["tailscaled.service" "netclode.service"];
    wants = ["tailscaled.service"];
    wantedBy = ["multi-user.target"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      # Wait for tailscale to be connected
      sleep 5

      status="$(${pkgs.tailscale}/bin/tailscale status --json 2>/dev/null | ${pkgs.jq}/bin/jq -r '.BackendState // "NoState"')"
      if [ "$status" = "Running" ]; then
        ${pkgs.tailscale}/bin/tailscale serve --bg --https=443 http://localhost:3000 || true
        echo "Tailscale serve configured for control plane"
      else
        echo "Tailscale not running, skipping serve setup"
      fi
    '';

    preStop = ''
      ${pkgs.tailscale}/bin/tailscale serve off || true
    '';
  };
}
