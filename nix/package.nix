{
  lib,
  stdenvNoCC,
  buildGoModule,
  go_1_27,
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

  # Same Go the dev shell asserts against go.mod.
  goVersion = builtins.head (builtins.match ".*\ngo ([0-9.]+)\n.*" (builtins.readFile ../go.mod));
  go = assert go_1_27.version == goVersion; go_1_27;

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
(buildGoModule.override { inherit go; }) {
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
