{
  description = "Agent-ergonomic local router CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv.url = "github:cachix/devenv";
    devenv.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, devenv, ... }@inputs:
    let
      version = "dev-${self.shortRev or self.dirtyShortRev or "dirty"}";
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
            inherit version;
            src = ./.;
            subPackages = [ "cmd/router-axi" ];
            vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
            ldflags = [ "-X main.version=${version}" ];
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
