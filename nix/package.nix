{
  lib,
  stdenvNoCC,
  buildGoModule,
  go,
  fetchPnpmDeps,
  pnpmConfigHook,
  pnpm_11,
  nodejs_24,
  rev ? "none",
  date ? "unknown",
}:

# Two derivations mirroring the Dockerfile stages: the Vite/React frontend,
# then the CGO-free Go binary that embeds it. Dependency hashes live in
# nix/hashes.json; refresh them with `make nix-hashes`.
let
  fs = lib.fileset;
  hashes = lib.importJSON ./hashes.json;

  # Minimum Go: the `go` directive in go.mod, raised to a `toolchain` line
  # if there is one. Any newer Go is accepted, so a host can follow its own
  # nixpkgs. buildGoModule alone would not give a readable error: it sets
  # GOTOOLCHAIN=local, so an older Go only fails deep in the build with
  # "go.mod requires go >= X". This check fails at evaluation instead.
  goMod = builtins.readFile ../go.mod;
  goDirective = builtins.head (builtins.match ".*\ngo ([0-9.]+)\n.*" goMod);
  toolchainLine = builtins.match ".*\ntoolchain go([0-9.]+)\n.*" goMod;
  goMinimum =
    if toolchainLine != null && lib.versionOlder goDirective (builtins.head toolchainLine) then
      builtins.head toolchainLine
    else
      goDirective;

  # `go` is the nixpkgs default Go, which tracks the newest stable release.
  # Pinning go_1_27 would break once nixpkgs drops that attribute, while the
  # default always satisfies a go.mod minimum that was current when written.
  goChecked =
    if lib.versionAtLeast go.version goMinimum then
      go
    else
      throw "dmanager needs Go >= ${goMinimum} (from go.mod), but this nixpkgs provides Go ${go.version}; use a newer nixpkgs.";

  version = "dev";
  shortRev = builtins.substring 0 7 rev;

  frontend = stdenvNoCC.mkDerivation (finalAttrs: {
    pname = "dmanager-frontend";
    inherit version;

    src = fs.toSource {
      root = ../frontend;
      fileset = fs.difference ../frontend (fs.unions [
        ../frontend/dist
        ../frontend/README.md
      ]);
    };

    pnpmDeps = fetchPnpmDeps {
      inherit (finalAttrs) pname version src;
      pnpm = pnpm_11;
      fetcherVersion = 5;
      hash = hashes.pnpm;
    };

    nativeBuildInputs = [
      nodejs_24
      pnpm_11
      pnpmConfigHook
    ];

    # `pnpm build` also regenerates the PWA icons; they are committed in
    # public/, so run only the TypeScript and Vite steps.
    buildPhase = ''
      runHook preBuild
      pnpm exec tsc -b
      pnpm exec vite build
      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      cp -r dist $out
      runHook postInstall
    '';
  });
in
(buildGoModule.override { go = goChecked; }) {
  pname = "dmanager";
  inherit version;

  src = fs.toSource {
    root = ../.;
    fileset = fs.unions [
      ../go.mod
      ../go.sum
      ../main.go
      ../cmd
      ../internal
    ];
  };

  vendorHash = hashes.go;

  subPackages = [ "." ];

  env.CGO_ENABLED = 0;

  ldflags = [
    "-s"
    "-w"
    "-X dmanager/internal/version.Version=${version}"
    "-X dmanager/internal/version.Commit=${shortRev}"
    "-X dmanager/internal/version.Date=${date}"
  ];

  # go:embed all:frontend/dist needs the built SPA at compile time.
  preBuild = ''
    mkdir -p frontend
    cp -r ${frontend} frontend/dist
  '';

  # The Go suite runs in CI.
  doCheck = false;

  passthru = { inherit frontend; };

  meta = {
    description = "Docker container update manager";
    homepage = "https://github.com/noosxe/dmanager";
    license = lib.licenses.mit;
    mainProgram = "dmanager";
    platforms = [ "x86_64-linux" "aarch64-linux" ];
  };
}
