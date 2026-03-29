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
            pkgs.gnumake
            pkgs.golangci-lint
            pkgs.gitleaks
            pkgs.yq-go
            stable.pre-commit
          ];

          shellHook = ''
            # Export GOROOT so Go works outside the devshell (e.g. Claude Code sandbox)
            export GOROOT="${pkgs.go}/share/go"

            # Use project-local GOBIN so Go-installed tools match the devshell Go version
            export GOBIN="$PWD/.gobin"
            export PATH="$GOBIN:$PATH"

            # Auto-install git hooks (one-time)
            _sentinel=".git/hooks/.aide-dev-setup-done"
            if [ ! -f "$_sentinel" ] && [ -d .git ] && [ -f .pre-commit-config.yaml ]; then
              pre-commit install --allow-missing-config -q 2>/dev/null
              pre-commit install --hook-type pre-push --allow-missing-config -q 2>/dev/null
              touch "$_sentinel" 2>/dev/null
            fi

            # Install Go security tools (built against the devshell Go version)
            _gobin_sentinel="$GOBIN/.installed-$(go version | awk '{print $3}')"
            if [ ! -f "$_gobin_sentinel" ]; then
              echo "Installing Go security tools for $(go version | awk '{print $3}')..."
              go install github.com/securego/gosec/v2/cmd/gosec@latest 2>/dev/null
              go install golang.org/x/vuln/cmd/govulncheck@latest 2>/dev/null
              touch "$_gobin_sentinel" 2>/dev/null
            fi

            echo "aide dev environment ready (Go $(go version | awk '{print $3}' | sed 's/go//'))"
          '';
        };
      }
    );
}
