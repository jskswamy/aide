# Contributing to aide

Welcome to aide. We appreciate contributions of all kinds — bug fixes, features, docs, and tests. Please read our [AI Policy](AI_POLICY.md) before contributing.

## Development Setup

### With Nix (recommended)

```sh
nix develop
```

This provides: Go, GNU Make, golangci-lint, gitleaks, yq-go, and pre-commit.

On shell entry, the devshell automatically:
- Installs git hooks (pre-commit and pre-push) via pre-commit
- Installs gosec and govulncheck to `.gobin/`

### Without Nix

Install the following manually:

| Tool | Minimum Version |
|------|----------------|
| Go | 1.25.7 |
| GNU Make | any |
| golangci-lint | 2.x |
| pre-commit | any |
| gitleaks | 8.x |

Then install git hooks:

```sh
pre-commit install
pre-commit install --hook-type pre-push
```

## Building and Testing

| Target | Command | Description |
|--------|---------|-------------|
| `all` | `make all` | Build, vet, and test (default) |
| `build` | `make build` | Compile binary to `bin/aide` |
| `install` | `make install` | `go install ./cmd/aide` |
| `test` | `make test` | Run unit tests |
| `test-integration` | `make test-integration` | Run integration tests (build tag `integration`) |
| `vet` | `make vet` | Run `go vet` |
| `lint` | `make lint` | Run golangci-lint |
| `gosec` | `make gosec` | Run gosec with exclusions from `.gosec.yaml` |
| `clean` | `make clean` | Remove `bin/` |
| `devcontainer-build` | `make devcontainer-build` | Build the Linux devcontainer image |
| `test-linux` | `make test-linux` | Run full test suite inside the Linux devcontainer |
| `test-all` | `make test-all` | Native tests + Linux container tests |

## Project Structure

```
cmd/aide/       CLI entry point
internal/       Private packages (config, context, launcher, sandbox, capability, trust, secrets, ui)
pkg/seatbelt/   Public Seatbelt profile library (reusable outside aide)
docs/           User-facing documentation
scripts/        Development scripts
testdata/       Test fixtures
```

See [DESIGN.md](DESIGN.md) for detailed architecture and package breakdown.

## Code Style

Standard Go conventions apply. All code must be gofmt-formatted.

golangci-lint enforces the following linters: **errcheck**, **govet**, **staticcheck**, **unused**, **misspell**, **revive**, **gocritic**, **exhaustive**, **nolintlint**.

Zero tolerance policy: `max-issues-per-linter: 0`, `max-same-issues: 0`. Fix all warnings.

### Pre-commit hooks

**On commit:**

| Hook | Source |
|------|--------|
| trailing-whitespace | pre-commit-hooks |
| end-of-file-fixer | pre-commit-hooks |
| check-yaml | pre-commit-hooks |
| check-merge-conflict | pre-commit-hooks |
| detect-private-key | pre-commit-hooks |
| doc-slop-check | local (`scripts/check-doc-slop.sh`) |
| golangci-lint | golangci/golangci-lint |
| go-build | dnephin/pre-commit-golang |

**On push:**

| Hook | Source |
|------|--------|
| gitleaks | gitleaks/gitleaks |
| go-unit-tests | dnephin/pre-commit-golang |

## Commit Message Convention

Follow the classic Git commit message style ([Chris Beams' 7 Rules](https://cbea.ms/git-commit/)):

1. Separate subject from body with a blank line
2. Limit subject to 50 characters
3. Capitalize the subject line
4. Do not end the subject with a period
5. Use imperative mood ("Add", "Fix", "Update" -- not "Added", "Fixes")
6. Wrap body at 72 characters
7. Use the body to explain what and why, not how

Do not use conventional-commit prefixes (`feat:`, `fix:`, `sec:`, `fix(scope):`). The 7 rules above are the whole spec.

Make atomic commits: one logical change per commit. Concretely:

- **One concern per commit.** Don't bundle a behaviour change with a doc-comment cleanup, or a security fix with an alphabetisation reorder. If the subject needs "and", split the commit.
- **Tests travel with their implementation.** Tests and the implementation they exercise belong in the same commit, regardless of which was written first. Fold them together before review.
- **No drive-by changes.** A commit on a feature branch should serve the feature. Unrelated tweaks belong in their own PR.

Keep commit messages clean. No AI co-author attributions, no task tracker IDs, no agent workflow artifacts. If someone with no knowledge of your tools would find a reference confusing, it does not belong in the commit message.

### Cleaning up before requesting review

Local TDD often produces granular ratcheting commits — introduce a function, fix a typo, adjust a test, expand the implementation. That's fine while you're working. Before opening the PR, fold the introduce-then-fix pairs into their target commits with `git commit --fixup=<hash>` and `git rebase -i --autosquash <base>`.

Heuristic: if commit B starts with "Fix", "Update", or "Correct" and shares a key noun with an earlier commit A on the same branch, B is almost certainly a fixup of A.

## Pull Requests

- PRs are rebase-merged. Each commit on the branch lands on `main` as-is, so every commit must stand on its own.
- Branch from `main`.
- Keep PRs focused on a single concern.
- Unrelated dependency bumps, formatting passes, and refactors go in separate PRs.
- No `fixup!` or `squash!` subjects in the pushed branch — autosquash before pushing.
- Describe what changed and why in the PR description.
- CI must pass before requesting review.
- Two-stage review: automated AI review first, then human review.

## Security

- gosec and gitleaks run in CI.
- Never commit plaintext secrets, `.env` files, or age private keys.
- Report security issues privately -- do not open a public issue.
