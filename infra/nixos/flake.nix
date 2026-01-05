{
  description = "Netclode - Self-hosted Claude Code Cloud";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.11";

    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = {
    self,
    nixpkgs,
    disko,
    ...
  }: let
    system = "x86_64-linux";
    pkgs = nixpkgs.legacyPackages.${system};
  in {
    # Host system configuration
    nixosConfigurations.netclode = nixpkgs.lib.nixosSystem {
      inherit system;
      modules = [
        disko.nixosModules.disko
        ./hosts/netclode-do
        ./modules/containerd.nix
        ./modules/juicefs.nix
        ./modules/tailscale.nix
        ./modules/nix-serve.nix
        ./modules/control-plane.nix
      ];
    };

    # Agent VM NixOS configuration (for building OCI image)
    nixosConfigurations.agent = nixpkgs.lib.nixosSystem {
      inherit system;
      modules = [
        ./agent
      ];
    };

    # Packages
    packages.${system} = {
      # Agent rootfs as a tarball (for OCI image)
      agent-rootfs = self.nixosConfigurations.agent.config.system.build.toplevel;

      # Agent OCI image for containerd
      agent-image = pkgs.callPackage ./agent/oci.nix {
        inherit (self.nixosConfigurations.agent) config;
        inherit pkgs;
      };
    };

    # Development shell
    devShells.${system}.default = pkgs.mkShell {
      packages = with pkgs; [
        bun
        nodejs_22
        nerdctl
        jq
        # For remote deployment
        nixos-rebuild
      ];

      shellHook = ''
        echo "Netclode development shell"
        echo "  - bun: $(bun --version)"
        echo "  - nix: $(nix --version)"
      '';
    };
  };
}
