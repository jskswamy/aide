# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd prime` or `bd ready` to inspect active work.

## Build, Test, and Quality Commands

```sh
make all              # Build binary, run go vet, and run unit tests with -race
make build            # Compile binary to bin/aide
make test/unit        # Run unit tests with race detector (go test -race ./...)
make test/coverage    # Run tests with coverage and enforce .testcoverage.yml thresholds
make lint             # Run golangci-lint (zero warnings policy)
make security/gosec   # Run gosec with exclusions from .gosec.yaml
make security/vuln    # Run govulncheck for known CVEs
```

## Branch, Pull Request, and Review Workflow

1. **Never commit or push directly to `main`**:
   - Create a short-lived branch from `main` for every feature, fix, or chore (`feat/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>`).
   - `.pre-commit-config.yaml` enforces `no-commit-to-branch` on `main`, and the GitHub `main` ruleset rejects direct pushes and merge commits.
2. **Keep documentation synchronized in the same PR**:
   - CLI command/flag changes in `cmd/aide/` must update `docs/cli-reference.md` (and `README.md` if top-level UX changed).
   - Config schema or profile changes in `internal/config/` must update `docs/configuration.md` or `docs/profiles.md`.
   - Capability or sandbox guard changes in `internal/capability/` or `internal/sandbox/` must update `docs/capabilities.md` or `docs/sandbox.md`.
   - Provisioning or skillshare changes in `internal/provision/` must update `docs/provisioning.md` or `docs/skillshare.md`.
   - Architecture or `DD-1..DD-33` changes must update `DESIGN.md`.
3. **Greptile Review & Rebase-Only Merge**:
   - Open a focused Pull Request against `main` (`gh pr create --fill`) so GitHub Actions CI and Greptile (`.greptile/`) run the 5-Boundary Sandbox Threat-Model Sweep, Ponytail review, refactoring scan, and doc-sync audit.
   - Use `/pr-review` (`.claude/commands/pr-review.md`) and `/ci-status` (`.claude/commands/ci-status.md`) to inspect feedback, fold fixups into atomic commits (`git rebase -i --autosquash origin/main`), and keep the branch rebased onto `main`. Never merge with GitHub's merge buttons; a maintainer merges by commenting `/merge` on the PR (see CONTRIBUTING.md "Pull Requests").
