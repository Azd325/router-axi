{
  description = "Agent-ergonomic local router CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv.url = "github:cachix/devenv";
    devenv.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { nixpkgs, devenv, ... }@inputs:
    let
      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-darwin"
        "x86_64-linux"
      ];
    in
    {
      packages = nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          router-axi = pkgs.buildGoModule {
            pname = "router-axi";
            version = "0.6.1";
            src = ./.;
            subPackages = [ "cmd/router-axi" ];
            vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
            ldflags = [ "-X main.version=v0.6.1" ];
            meta = {
              description = "Agent-ergonomic local router CLI";
              homepage = "https://github.com/Azd325/router-axi";
              license = pkgs.lib.licenses.mit;
              mainProgram = "router-axi";
            };
          };
        in
        {
          inherit router-axi;
          default = router-axi;
        });

      devShells = nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = devenv.lib.mkShell {
            inherit inputs pkgs;
            modules = [ ./devenv.nix ];
          };
        });
    };
}
