# k3s with Kata Containers (Cloud Hypervisor) configuration
{
  config,
  lib,
  pkgs,
  ...
}: let
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

  # containerd config template for k3s with Kata runtime
  containerdConfigTmpl = pkgs.writeText "config.toml.tmpl" ''
    version = 2

    [plugins."io.containerd.grpc.v1.cri".containerd]
      default_runtime_name = "runc"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
        runtime_type = "io.containerd.runc.v2"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.kata-clh]
        runtime_type = "io.containerd.kata-clh.v2"
        privileged_without_host_devices = true
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.kata-clh.options]
          ConfigPath = "/etc/kata-containers/configuration-clh.toml"
  '';
in {
  # Enable k3s
  services.k3s = {
    enable = true;
    role = "server";
    extraFlags = toString [
      "--disable=traefik"
      "--disable=servicelb"
      "--flannel-backend=host-gw"
    ];
  };

  # k3s service configuration for Kata
  systemd.services.k3s = {
    path = [pkgs.kata-runtime pkgs.nftables];
    serviceConfig = {
      # Device access for Kata VMs and kubelet
      DeviceAllow = [
        "/dev/kvm rwm"
        "/dev/vhost-vsock rwm"
        "/dev/vhost-net rwm"
        "/dev/net/tun rwm"
        "/dev/kmsg r"
      ];
      Delegate = "yes";
      # Allow access to kernel logs (kubelet needs this)
      ProtectKernelLogs = false;
    };
  };

  # Install packages
  environment.systemPackages = with pkgs; [
    kubectl
    k9s
    kata-runtime
    cloud-hypervisor
    virtiofsd
  ];

  # Kata configuration
  environment.etc."kata-containers/configuration-clh.toml".source = kataConfig;

  # containerd config template for k3s
  # k3s reads this from /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl
  systemd.tmpfiles.rules = [
    "d /var/lib/kata 0755 root root -"
    "d /var/lib/rancher/k3s/agent/etc/containerd 0755 root root -"
    "L+ /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl - - - - ${containerdConfigTmpl}"
  ];

  # Service to download Kata assets if not present
  systemd.services.kata-assets = {
    description = "Download Kata Containers assets";
    wantedBy = ["multi-user.target"];
    before = ["k3s.service"];

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

  # KVM kernel modules
  boot.kernelModules = ["kvm-intel" "kvm-amd" "vhost_net"];

  # Open k3s API port on Tailscale
  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [6443];
}
