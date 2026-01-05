# Netclode control plane service
#
# Expects:
#   /opt/netclode - Application code (synced via rsync or git)
#   /var/secrets/netclode.env - Environment variables (ANTHROPIC_API_KEY, etc.)
#
{
  config,
  lib,
  pkgs,
  ...
}: {
  # Control plane systemd service
  systemd.services.netclode = {
    description = "Netclode Control Plane";
    after = [
      "network-online.target"
      "containerd.service"
      "juicefs.service"
    ];
    wants = ["network-online.target"];
    requires = ["containerd.service" "juicefs.service"];
    wantedBy = ["multi-user.target"];

    environment = {
      NODE_ENV = "production";
      JUICEFS_ROOT = "/juicefs";
      PORT = "3000";
    };

    serviceConfig = {
      Type = "simple";
      User = "root";
      Group = "root";

      WorkingDirectory = "/opt/netclode";
      ExecStart = "${pkgs.bun}/bin/bun run apps/control-plane/src/index.ts";

      Restart = "always";
      RestartSec = "5s";

      # Environment file for secrets
      EnvironmentFile = "/var/secrets/netclode.env";

      # Logging
      StandardOutput = "journal";
      StandardError = "journal";
      SyslogIdentifier = "netclode";
    };
  };

  # Service to pull agent image on boot
  systemd.services.netclode-pull-image = {
    description = "Pull Netclode Agent Image";
    after = ["containerd.service" "network-online.target"];
    wants = ["network-online.target"];
    before = ["netclode.service"];
    wantedBy = ["multi-user.target"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      # Pull agent image if configured
      if [ -n "''${AGENT_IMAGE:-}" ]; then
        echo "Pulling agent image: $AGENT_IMAGE"
        ${pkgs.nerdctl}/bin/nerdctl pull "$AGENT_IMAGE" || true
      fi
    '';

    environment = {
      AGENT_IMAGE = ""; # Set via EnvironmentFile if needed
    };
  };

  # Log directory
  systemd.tmpfiles.rules = [
    "d /var/log/netclode 0750 root root -"
  ];
}
