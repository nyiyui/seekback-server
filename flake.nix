{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }@attrs:
    let
      pkgs = import nixpkgs { config.allowUnfree = true; };
    in
    flake-utils.lib.eachSystem flake-utils.lib.defaultSystems (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        build =
          pkgs:
          (pkgs.buildGoModule rec {
            pname = "seekback-server";
            version = if (self ? rev) then self.rev else "dirty";
            src = ./.;
            vendorHash = "sha256-LFK6qrNw4NUBPcGCbgvFeH0QGSKoS054y+OcxMm+w6M=";
            subPackages = [ "cmd/server" ];
          });
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            govulncheck
            nixfmt-rfc-style
            sqlite
            sqlitebrowser
          ];
        };
        packages.default = build pkgs;
      }
    );
}
