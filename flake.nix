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
            vendorHash = "sha256-LOlkBswmqnfAHLxA0x0fwduT5tYz32OZKuBGNoHhxb0=";
            subPackages = [ "cmd/server" ];
            tags = [ "fts5" ];
            ldflags = [ "-X nyiyui.ca/seekback-server/server.vcsInfo=${version}" ];
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
