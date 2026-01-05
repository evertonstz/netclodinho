# Agent VM NixOS configuration
#
# This configuration is used to build the OCI image that runs inside
# Kata Containers (Cloud Hypervisor) VMs.
#
{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}: {
  imports = [
    (modulesPath + "/profiles/minimal.nix")
  ];

  # System basics
  system.stateVersion = "24.11";

  # Don't need a bootloader (Kata provides kernel)
  boot.loader.grub.enable = false;
  boot.isContainer = true;

  # Minimal kernel config
  boot.kernelModules = ["overlay" "br_netfilter"];

  # Networking (DHCP from CNI)
  networking = {
    hostName = "agent";
    useDHCP = true;
    firewall.enable = false;
  };

  # Nix configuration - use host binary cache
  nix = {
    enable = true;
    settings = {
      experimental-features = ["nix-command" "flakes"];

      # Host's nix-serve at gateway IP (set by CNI)
      substituters = [
        "http://10.88.0.1:5000"
        "https://cache.nixos.org"
      ];

      trusted-public-keys = [
        "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
        # Host key will be added at runtime or build time
      ];
    };
  };

  # Docker daemon for container workloads
  virtualisation.docker = {
    enable = true;
    autoPrune = {
      enable = true;
      dates = "daily";
    };
  };

  # Essential packages
  environment.systemPackages = with pkgs; [
    # Runtime
    bun
    nodejs_22

    # Dev tools
    git
    gh
    curl
    wget
    jq
    ripgrep
    fd
    tree

    # Build tools
    gnumake
    gcc

    # Nix tools for dynamic deps
    nix-direnv
  ];

  # Agent user
  users.users.agent = {
    isNormalUser = true;
    home = "/home/agent";
    extraGroups = ["docker"];
    shell = pkgs.bash;
  };

  # Agent service
  systemd.services.agent = {
    description = "Netclode Agent";
    after = ["network-online.target" "docker.service"];
    wants = ["network-online.target"];
    wantedBy = ["multi-user.target"];

    environment = {
      HOME = "/home/agent";
      WORKSPACE = "/workspace";
      NODE_ENV = "production";
    };

    serviceConfig = {
      Type = "simple";
      User = "agent";
      Group = "users";
      WorkingDirectory = "/opt/agent";
      ExecStart = "${pkgs.bun}/bin/bun run src/index.ts";
      Restart = "always";
      RestartSec = "2s";

      # Secrets from environment file (mounted by host)
      EnvironmentFile = "-/run/secrets/agent.env";

      StandardOutput = "journal";
      StandardError = "journal";
    };
  };

  # Workspace mount point
  systemd.tmpfiles.rules = [
    "d /workspace 0755 agent users -"
    "d /opt/agent 0755 agent users -"
  ];

  # Timezone
  time.timeZone = "UTC";

  # Locale
  i18n.defaultLocale = "en_US.UTF-8";

  # Disable unnecessary services for minimal image
  services.udisks2.enable = false;
  security.polkit.enable = false;
  programs.command-not-found.enable = false;

  # SSH for debugging (optional, can be disabled)
  services.openssh = {
    enable = true;
    settings = {
      PermitRootLogin = "no";
      PasswordAuthentication = false;
    };
  };
}
