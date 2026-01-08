# k3s with Kata Containers (Firecracker) configuration
#
# Why Kata static release instead of NixOS packages:
# - containerd 2.x (bundled with k3s 1.34+) changed how CRI creates pod sandboxes
# - NixOS's kata-runtime package works with `ctr run` but fails with CRI's RunPodSandbox
# - Error: "failed to mount kataShared to /run/kata-containers/shared/containers/: EINVAL"
# - The Kata static release includes pre-configured, compatible binaries that work together
# - nix-ld is required to run the dynamically linked Kata binaries on NixOS
{
  config,
  lib,
  pkgs,
  ...
}: let
  kataVersion = "3.16.0";
  kataDir = "/opt/kata";

  # containerd config template for k3s with Kata runtime
  # Uses Go templating - k3s replaces {{ .NodeConfig.* }} variables
  # Points to Kata static release binaries
  containerdConfigTmpl = pkgs.writeText "config.toml.tmpl" ''
    version = 2

    [plugins."io.containerd.grpc.v1.cri"]
      [plugins."io.containerd.grpc.v1.cri".cni]
        bin_dir = "{{ .NodeConfig.AgentConfig.CNIBinDir }}"
        conf_dir = "{{ .NodeConfig.AgentConfig.CNIConfDir }}"

      [plugins."io.containerd.grpc.v1.cri".containerd]
        default_runtime_name = "runc"

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
          runtime_type = "io.containerd.runc.v2"

        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.kata-fc]
          runtime_type = "io.containerd.kata-fc.v2"
          privileged_without_host_devices = true
          pod_annotations = ["io.katacontainers.*"]
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.kata-fc.options]
            ConfigPath = "${kataDir}/share/defaults/kata-containers/configuration-fc.toml"
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
    path = [pkgs.nftables];
    environment = {
      # Add Kata static release binaries to PATH
      PATH = lib.mkForce "${kataDir}/bin:/run/current-system/sw/bin:/run/wrappers/bin";
    };
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

  # Install packages (Kata comes from static release)
  environment.systemPackages = with pkgs; [
    kubectl
    k9s
  ];

  # containerd config template for k3s
  # k3s reads this from /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl
  systemd.tmpfiles.rules = [
    "d /var/lib/rancher/k3s/agent/etc/containerd 0755 root root -"
    "L+ /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl - - - - ${containerdConfigTmpl}"
  ];

  # Service to download and install Kata static release
  systemd.services.kata-install = {
    description = "Install Kata Containers static release";
    wantedBy = ["multi-user.target"];
    before = ["k3s.service"];

    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };

    script = ''
      set -euo pipefail

      KATA_VERSION="${kataVersion}"
      KATA_DIR="${kataDir}"

      # Check if already installed with correct version
      if [ -f "$KATA_DIR/bin/kata-runtime" ] && [ -f "$KATA_DIR/.version" ] && [ "$(cat $KATA_DIR/.version 2>/dev/null)" = "$KATA_VERSION" ]; then
        echo "Kata $KATA_VERSION already installed"
        exit 0
      fi

      echo "Installing Kata Containers $KATA_VERSION..."

      # Download and extract
      ${pkgs.curl}/bin/curl -fsSL \
        "https://github.com/kata-containers/kata-containers/releases/download/$KATA_VERSION/kata-static-$KATA_VERSION-amd64.tar.xz" \
        | ${pkgs.xz}/bin/xz -d | ${pkgs.gnutar}/bin/tar -xf - -C /

      # Create symlinks for containerd to find the shim
      # Link to /usr/local/bin (create if needed) and k3s bin directory
      mkdir -p /usr/local/bin
      ln -sf $KATA_DIR/bin/containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-v2
      ln -sf $KATA_DIR/bin/containerd-shim-kata-v2 /usr/local/bin/containerd-shim-kata-fc-v2

      # Also link to k3s bin directory (use wildcard since version changes)
      for k3s_bin in /var/lib/rancher/k3s/data/*/bin; do
        if [ -d "$k3s_bin" ]; then
          ln -sf $KATA_DIR/bin/containerd-shim-kata-v2 "$k3s_bin/containerd-shim-kata-v2"
          ln -sf $KATA_DIR/bin/containerd-shim-kata-v2 "$k3s_bin/containerd-shim-kata-fc-v2"
        fi
      done

      echo "$KATA_VERSION" > "$KATA_DIR/.version"
      echo "Kata Containers $KATA_VERSION installed"
    '';
  };

  # KVM kernel modules
  boot.kernelModules = ["kvm-intel" "kvm-amd" "vhost_net"];

  # Open k3s API port on Tailscale
  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [6443];
}
