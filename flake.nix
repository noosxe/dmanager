{
  description = "A Nix dev shell with pinned latest stable Go, LTS Node, and PNPM";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        # Latest LTS NodeJS (Node 24)
        nodejs = pkgs.nodejs_24;

        # Latest stable PNPM overridden with our pinned LTS Node
        pnpm = pkgs.pnpm.override { nodejs-slim = nodejs; };

        # Go pinned to the exact repo-wide version. Bump in lockstep with
        # go.mod, .github/workflows/backend.yml, and the Dockerfile; the
        # assertion fails the dev shell if nixpkgs drifts to another patch.
        go = assert pkgs.go_1_27.version == "1.27.1"; pkgs.go_1_27;
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            go
            nodejs
            pnpm
            pkgs.golangci-lint
            pkgs.direnv
            pkgs.nix-direnv
            pkgs.sqlc
            pkgs.goose
            pkgs.buf
            pkgs.protoc-gen-go
            pkgs.protoc-gen-connect-go
          ];

          shellHook = ''
            echo "========================================="
            echo "   dmanager Nix Dev Shell Activated"
            echo "========================================="
            echo "Go Version:   $(go version | awk '{print $3}')"
            echo "Node Version: $(node --version)"
            echo "PNPM Version: $(pnpm --version)"
            echo "========================================="
          '';
        };
      });
}
