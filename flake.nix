{
  description = "aide — Universal Coding Agent Context Manager";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    nixpkgs-stable.url = "github:NixOS/nixpkgs/nixos-24.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, nixpkgs-stable, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        stable = nixpkgs-stable.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.golangci-lint
            pkgs.gosec
            pkgs.gitleaks
            stable.pre-commit
          ];

          shellHook = ''
            # Export GOROOT so Go works outside the devshell (e.g. Claude Code sandbox)
            export GOROOT="${pkgs.go}/share/go"

            # Auto-install git hooks (one-time)
            _sentinel=".git/hooks/.aide-dev-setup-done"
            if [ ! -f "$_sentinel" ] && [ -d .git ] && [ -f .pre-commit-config.yaml ]; then
              pre-commit install --allow-missing-config -q 2>/dev/null
              pre-commit install --hook-type pre-push --allow-missing-config -q 2>/dev/null
              touch "$_sentinel" 2>/dev/null
            fi

            # Install Go tools not available in nixpkgs
            if ! command -v govulncheck &>/dev/null; then
              echo "Installing govulncheck..."
              go install golang.org/x/vuln/cmd/govulncheck@latest 2>/dev/null
            fi

            echo "aide dev environment ready (Go $(go version | awk '{print $3}' | sed 's/go//'))"
          '';
        };
      }
    );
}
