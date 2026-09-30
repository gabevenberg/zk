{
  description = "zk devshell";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs = {nixpkgs, ...}: let
    forAllSystems = function:
      nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ] (system:
        function {
          pkgs = import nixpkgs {inherit system;};
          inherit system;
        });
  in {
    devShells = forAllSystems ({pkgs, ...}: {
      # mkShell's stdenv supplies gcc and make, which the cgo sqlite driver and the Makefile need.
      default = pkgs.mkShell {
        buildInputs = with pkgs; [
          # go.mod wants >= 1.25, which nixpkgs has dropped as end-of-life.
          go
        ];
      };
    });
  };
}
