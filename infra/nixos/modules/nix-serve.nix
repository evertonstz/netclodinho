# Nix binary cache for agent VMs
#
# VMs fetch packages from this cache instead of cache.nixos.org
# This speeds up VM boot and reduces external bandwidth
#
{
  config,
  lib,
  pkgs,
  ...
}: {
  # Generate signing key if not exists
  systemd.services.nix-serve-keygen = {
    description = "Generate nix-serve signing key";
    wantedBy = ["multi-user.target"];
    before = ["nix-serve.service"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      if [ ! -f /var/secrets/nix-serve-private-key ]; then
        echo "Generating nix-serve signing key..."
        ${pkgs.nix}/bin/nix-store --generate-binary-cache-key \
          netclode-cache \
          /var/secrets/nix-serve-private-key \
          /var/secrets/nix-serve-public-key
        chmod 600 /var/secrets/nix-serve-private-key
        chmod 644 /var/secrets/nix-serve-public-key
        echo "Key generated. Public key:"
        cat /var/secrets/nix-serve-public-key
      fi
    '';
  };

  # nix-serve binary cache
  services.nix-serve = {
    enable = true;
    port = 5000;
    bindAddress = "10.88.0.1"; # Only accessible from VM network
    secretKeyFile = "/var/secrets/nix-serve-private-key";
  };

  # Ensure nix-serve waits for key generation
  systemd.services.nix-serve = {
    after = ["nix-serve-keygen.service"];
    requires = ["nix-serve-keygen.service"];
  };
}
