{
  description = "A Nix-flake-based Go 1.23 development environment";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
  };

  outputs = {
    self,
    nixpkgs,
    ...
  }: let
    goVersion = 23; # Change this to update the whole stack
    pkgs = nixpkgs.legacyPackages.x86_64-linux;
  in {
    overlays.default = final: prev: {
      go = final."go_1_${toString goVersion}";
    };

    devShells.x86_64-linux.default = pkgs.mkShell {
      hardeningDisable = ["fortify"];
      packages = with pkgs; [go gotools golangci-lint];
    };
  };
}
