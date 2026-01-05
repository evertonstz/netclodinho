# containerd with Kata Containers (Cloud Hypervisor) configuration
{
  config,
  lib,
  pkgs,
  ...
}: let
  # CNI config for VM networking
  cniConfig = pkgs.writeText "10-netclode.conflist" (builtins.toJSON {
    cniVersion = "1.0.0";
    name = "netclode";
    plugins = [
      {
        type = "bridge";
        bridge = "cni0";
        isGateway = true;
        ipMasq = true;
        ipam = {
          type = "host-local";
          ranges = [[{subnet = "10.88.0.0/16";}]];
          routes = [{dst = "0.0.0.0/0";}];
        };
      }
      {
        type = "portmap";
        capabilities = {portMappings = true;};
      }
    ];
  });

  # Kata configuration for Cloud Hypervisor
  kataConfig = pkgs.writeText "configuration-clh.toml" ''
    [hypervisor.clh]
    path = "${pkgs.cloud-hypervisor}/bin/cloud-hypervisor"
    kernel = "/var/lib/kata/vmlinux"
    image = "/var/lib/kata/kata-containers.img"

    # Use virtio-fs for shared filesystem
    shared_fs = "virtio-fs"
    virtio_fs_daemon = "${pkgs.virtiofsd}/bin/virtiofsd"
    virtio_fs_cache_size = 1024
    virtio_fs_cache = "always"

    # VM defaults
    default_vcpus = 2
    default_memory = 2048
    default_maxmemory = 8192

    # Enable memory hotplug
    enable_mem_prealloc = false
    memory_slots = 10

    # Networking
    enable_iothreads = true

    [agent.kata]
    kernel_modules = []

    [runtime]
    enable_debug = false
    internetworking_model = "tcfilter"
    disable_new_netns = false
    sandbox_cgroup_only = true
  '';
in {
  # Enable containerd
  virtualisation.containerd = {
    enable = true;
    settings = {
      version = 2;
      plugins = {
        "io.containerd.grpc.v1.cri" = {
          containerd = {
            default_runtime_name = "kata-clh";
            runtimes = {
              kata-clh = {
                runtime_type = "io.containerd.kata-clh.v2";
                privileged_without_host_devices = true;
                options = {
                  ConfigPath = "/etc/kata-containers/configuration-clh.toml";
                };
              };
              # Keep runc for utility containers
              runc = {
                runtime_type = "io.containerd.runc.v2";
              };
            };
          };
          cni = {
            bin_dir = "${pkgs.cni-plugins}/bin";
            conf_dir = "/etc/cni/net.d";
          };
        };
      };
    };
  };

  # Install packages
  environment.systemPackages = with pkgs; [
    nerdctl
    cni-plugins
    kata-containers
    cloud-hypervisor
    virtiofsd
  ];

  # CNI configuration
  environment.etc."cni/net.d/10-netclode.conflist".source = cniConfig;

  # Kata configuration
  environment.etc."kata-containers/configuration-clh.toml".source = kataConfig;

  # Kata assets directory
  systemd.tmpfiles.rules = [
    "d /var/lib/kata 0755 root root -"
  ];

  # Service to download Kata assets if not present
  systemd.services.kata-assets = {
    description = "Download Kata Containers assets";
    wantedBy = ["multi-user.target"];
    before = ["containerd.service"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      set -euo pipefail

      KATA_VERSION="3.10.0"
      ASSETS_DIR="/var/lib/kata"

      # Download kernel if not present
      if [ ! -f "$ASSETS_DIR/vmlinux" ]; then
        echo "Downloading Kata kernel..."
        ${pkgs.curl}/bin/curl -fsSL \
          "https://github.com/kata-containers/kata-containers/releases/download/$KATA_VERSION/kata-static-$KATA_VERSION-x86_64.tar.xz" \
          | ${pkgs.gnutar}/bin/tar -xJf - -C /tmp

        cp /tmp/opt/kata/share/kata-containers/vmlinux.container "$ASSETS_DIR/vmlinux"
        cp /tmp/opt/kata/share/kata-containers/kata-containers.img "$ASSETS_DIR/kata-containers.img"
        rm -rf /tmp/opt

        echo "Kata assets downloaded"
      else
        echo "Kata assets already present"
      fi
    '';
  };

  # Open port on CNI bridge for nix-serve
  networking.firewall.interfaces."cni0".allowedTCPPorts = [5000];
}
