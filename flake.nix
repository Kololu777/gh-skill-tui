{
  description = "A lazygit-like TUI for installing and managing Agent Skills";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-darwin"
        "x86_64-linux"
      ];

      forEachSystem = nixpkgs.lib.genAttrs systems;
    in {
      packages = forEachSystem (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in {
          default = pkgs.callPackage ./package.nix { };
        });

      apps = forEachSystem (system:
        let
          pkgs = import nixpkgs { inherit system; };
          package = self.packages.${system}.default;
          launcher = pkgs.writeShellApplication {
            name = "gh-skill-tui";
            runtimeInputs = [ pkgs.gh pkgs.git ];
            text = ''
              exec ${package}/bin/gh-skill-tui "$@"
            '';
          };
        in {
          default = {
            type = "app";
            program = "${launcher}/bin/gh-skill-tui";
            meta.description = "A TUI for installing and managing Agent Skills";
          };
        });

      devShells = forEachSystem (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gh
              git
              golangci-lint
              actionlint
            ];
          };
        });
    };
}
