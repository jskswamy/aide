{
  description = "aide — Universal Coding Agent Context Manager";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    nixpkgs-stable.url = "github:NixOS/nixpkgs/nixos-24.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-stable,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        stable = nixpkgs-stable.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule.override { go = pkgs.go_1_26; } {
          pname = "aide";
          version = self.shortRev or self.dirtyShortRev or "dev";
          src = self;
          subPackages = [ "cmd/aide" ];
          vendorHash = "sha256-RyWLbNrm9pEMx/TsoGHRGkZEq/cQgAxfhWPYuaadRgM=";

          # ponytail: tests run via CI/pre-commit; skipping here avoids pulling
          # git+perl+python3 into the build closure just for two git-shelling tests.
          doCheck = false;

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${self.shortRev or self.dirtyShortRev or "dev"}"
            "-X main.commit=${self.shortRev or self.dirtyShortRev or "none"}"
          ];

          meta = {
            description = "aide — Universal Coding Agent Context Manager";
            homepage = "https://github.com/jskswamy/aide";
            mainProgram = "aide";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go_1_26
            pkgs.gnumake
            pkgs.golangci-lint
            pkgs.gosec
            pkgs.govulncheck
            pkgs.gitleaks
            pkgs.yq-go
            stable.pre-commit
          ];

          shellHook = ''
            # Export GOROOT so Go works outside the devshell (e.g. Claude Code sandbox)
            export GOROOT="${pkgs.go_1_26}/share/go"

            # Use project-local GOBIN so Go-installed tools match the devshell Go version
            export GOBIN="$PWD/.gobin"
            export PATH="$GOBIN:$PATH"

            # Install pre-commit hooks when no custom hooksPath is active (e.g. beads).
            # beads users invoke pre-commit via their own hook chain; everyone else
            # gets it wired up here automatically.
            if ! git config --local core.hooksPath >/dev/null 2>&1 && [ -f .pre-commit-config.yaml ]; then
              _sentinel=".git/hooks/.pre-commit-installed"
              if [ ! -f "$_sentinel" ]; then
                pre-commit install -q 2>/dev/null && \
                pre-commit install --hook-type pre-push -q 2>/dev/null && \
                touch "$_sentinel" 2>/dev/null || true
              fi
            fi

            echo "aide dev environment ready (Go $(go version | awk '{print $3}' | sed 's/go//'))"
          '';
        };
      }
    );
}
