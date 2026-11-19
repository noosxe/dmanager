# Assembles the flake outputs from the files in this directory.
{ self, nixpkgs, flake-utils }:
let
  overlay = final: _prev: {
    dmanager = final.callPackage ./package.nix {
      # Build metadata for the version ldflags; a dirty tree has no rev.
      rev = self.rev or self.dirtyRev or "none";
      date =
        let d = self.lastModifiedDate or "";
        in if d == "" then "unknown" else
          "${builtins.substring 0 4 d}-${builtins.substring 4 2 d}-${builtins.substring 6 2 d}T${builtins.substring 8 2 d}:${builtins.substring 10 2 d}:${builtins.substring 12 2 d}Z";
    };
  };
in
{
  overlays.default = overlay;
}
// flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" ] (system:
  let
    pkgs = import nixpkgs {
      inherit system;
      overlays = [ overlay ];
    };
  in
  {
    packages = {
      default = pkgs.dmanager;
      dmanager = pkgs.dmanager;
    };

    devShells.default = import ./devshell.nix { inherit pkgs; };
  })
