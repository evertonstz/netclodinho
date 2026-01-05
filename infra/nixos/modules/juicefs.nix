# JuiceFS configuration for session storage
#
# Expects secrets at:
#   /var/secrets/juicefs.env - Contains JUICEFS_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
#
{
  config,
  lib,
  pkgs,
  ...
}: {
  # JuiceFS package
  environment.systemPackages = [pkgs.juicefs];

  # JuiceFS mount service
  systemd.services.juicefs = {
    description = "JuiceFS Mount";
    after = ["network-online.target"];
    wants = ["network-online.target"];
    wantedBy = ["multi-user.target"];

    serviceConfig = {
      Type = "simple";
      Restart = "on-failure";
      RestartSec = "5s";
      EnvironmentFile = "/var/secrets/juicefs.env";
    };

    preStart = ''
      # Format if not already formatted (idempotent)
      if ! ${pkgs.juicefs}/bin/juicefs status sqlite3:///var/lib/juicefs/meta.db 2>/dev/null; then
        echo "Formatting JuiceFS filesystem..."
        ${pkgs.juicefs}/bin/juicefs format \
          --storage s3 \
          --bucket "$JUICEFS_BUCKET" \
          sqlite3:///var/lib/juicefs/meta.db \
          netclode
      fi
    '';

    script = ''
      exec ${pkgs.juicefs}/bin/juicefs mount \
        --cache-dir /var/cache/juicefs \
        --cache-size 50000 \
        --writeback \
        --no-bgjob \
        sqlite3:///var/lib/juicefs/meta.db \
        /juicefs
    '';

    postStop = ''
      ${pkgs.util-linux}/bin/umount /juicefs || true
    '';
  };

  # Create directories
  systemd.tmpfiles.rules = [
    "d /var/lib/juicefs 0750 root root -"
    "d /var/cache/juicefs 0750 root root -"
    "d /juicefs 0755 root root -"
    "d /juicefs/sessions 0755 root root -"
  ];
}
