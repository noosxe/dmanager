{ pkgs }:
let
  # Latest LTS NodeJS (Node 24)
  nodejs = pkgs.nodejs_24;

  # Latest stable PNPM overridden with our pinned LTS Node
  pnpm = pkgs.pnpm.override { nodejs-slim = nodejs; };

  # Go and golangci-lint versions are read from the files that CI and
  # the Makefile use, so the dev shell cannot drift from them. The
  # assertions fail the shell if the locked nixpkgs provides another
  # version; fix that with `nix flake update nixpkgs`.
  goVersion = builtins.head
    (builtins.match ".*\ngo ([0-9.]+)\n.*" (builtins.readFile ../go.mod));
  lintVersion = builtins.head
    (builtins.match ".*\nGOLANGCI_LINT_VERSION [?]= v([0-9.]+)\n.*"
      (builtins.readFile ../Makefile));
  go = assert pkgs.go_1_27.version == goVersion; pkgs.go_1_27;
  golangci-lint = assert pkgs.golangci-lint.version == lintVersion; pkgs.golangci-lint;
in
pkgs.mkShell {
  buildInputs = [
    go
    nodejs
    pnpm
    golangci-lint
    pkgs.jq
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
}
