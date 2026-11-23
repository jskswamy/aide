# Contributing to aide

Welcome to aide. We appreciate contributions of all kinds - bug fixes, features, docs, and tests. Please read our [AI Policy](AI_POLICY.md) before contributing.

## Development Setup

### With Nix (recommended)

```sh
nix develop
```

This provides: Go, GNU Make, golangci-lint, gosec, govulncheck, gitleaks, yq-go, and pre-commit.

On shell entry, the devshell automatically:
- Sets `GOBIN` to a project-local `.gobin/` so Go-installed tools match the devshell Go version
- Installs git hooks (pre-commit and pre-push) via pre-commit

### Without Nix

Install the following manually:

| Tool | Minimum Version |
|------|----------------|
| Go | 1.26 |
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
| `install` | `make install` | Install via goreleaser (matches release binary exactly) |
| `install-dev` | `make install-dev` | Install quickly without goreleaser (dev iteration) |
| `test/unit` | `make test/unit` | Run unit tests with race detector |
| `test/coverage` | `make test/coverage` | Run tests with coverage and enforce thresholds |
| `test/sandbox` | `make test/sandbox` | Run Linux sandbox unit tests (no integration tag) |
| `test/integration` | `make test/integration` | Run integration tests (build tag `integration`) |
| `vet` | `make vet` | Run `go vet` |
| `generate` | `make generate` | Regenerate mocks and other generated code |
| `lint` | `make lint` | Run golangci-lint |
| `security/gosec` | `make security/gosec` | Run gosec (`SARIF=1` for CI output) with exclusions from `.gosec.yaml` |
| `security/vuln` | `make security/vuln` | Run govulncheck for known CVEs |
| `clean` | `make clean` | Remove build artifacts and coverage output |
| `devcontainer-build` | `make devcontainer-build` | Build the Linux devcontainer image |
| `test/linux` | `make test/linux` | Run full test suite inside the Linux devcontainer |
| `test/all` | `make test/all` | Native tests + Linux container tests |

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
| no-commit-to-branch | pre-commit-hooks (blocks direct commits to `main`) |
| trailing-whitespace | pre-commit-hooks |
| end-of-file-fixer | pre-commit-hooks |
| check-yaml | pre-commit-hooks |
| check-merge-conflict | pre-commit-hooks |
| detect-private-key | pre-commit-hooks |
| actionlint | rhysd/actionlint |
| doc-slop-check | local (`scripts/check-doc-slop.sh`) |
| go-build | dnephin/pre-commit-golang |
| bd-pre-commit | local (`bd hooks run pre-commit` when `bd` is installed) |

**On push:**

| Hook | Source |
|------|--------|
| golangci-lint | golangci/golangci-lint |
| gosec | local (`make security/gosec`) |
| gitleaks | gitleaks/gitleaks |
| go-unit-tests | dnephin/pre-commit-golang |
| bd-pre-push | local (`bd hooks run pre-push` when `bd` is installed) |

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

Local TDD often produces granular ratcheting commits - introduce a function, fix a typo, adjust a test, expand the implementation. That's fine while you're working. Before opening the PR, fold the introduce-then-fix pairs into their target commits with `git commit --fixup=<hash>` and `git rebase -i --autosquash <base>`.

Heuristic: if commit B starts with "Fix", "Update", or "Correct" and shares a key noun with an earlier commit A on the same branch, B is almost certainly a fixup of A.

## Pull Requests

- `main` only moves by fast-forward, so it keeps a linear history where every commit stands on its own and keeps its author's signature. Do not use GitHub's merge buttons: "Rebase and merge" rewrites the commits and drops their signatures, and squash and merge commits are disabled.
- Branch from `main` using short-lived branches (`feat/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>`). Direct commits to `main` are blocked.
- Keep the branch rebased onto `main`: `git fetch origin && git rebase origin/main && git push --force-with-lease`. The "Rebased on main" check fails while the branch is behind `main` or contains merge commits. Rebase your own branch, so your own key signs it; outside contributors must sign their commits.
- To merge, a maintainer comments `/merge` on the PR. `.github/workflows/merge.yml` checks that the commenter has maintain or admin access, the PR is open and not a draft, required checks pass, every review thread is resolved and the branch is a fast-forward of `main`. It then pushes the PR's head commit to `main`, so GitHub marks the PR merged, and deletes the branch. On failure it comments with a link to the run.
- The workflow pushes as GitHub Actions, which is a bypass actor on the `main` ruleset; the separate no-force-push rule still allows only fast-forwards. If the workflow is unavailable, a maintainer can do the same by hand after checking the PR: `git push origin <head-sha>:main`.
- Keep PRs focused on a single concern.
- Update corresponding user and architecture documentation (`docs/*.md`, `README.md`, `DESIGN.md`) in the same PR when flags, config schema, capabilities, or design decisions change.
- Unrelated dependency bumps, formatting passes, and refactors go in separate PRs.
- No `fixup!` or `squash!` subjects in the pushed branch - autosquash before pushing.
- Describe what changed and why in the PR description.
- CI must pass before requesting review.
- Two-stage review: automated AI review via Greptile (`.greptile/` threat-model, Ponytail, refactoring, and doc-sync skills) first, then human review.

## Releases

aide follows [semantic versioning](https://semver.org/), with a bias towards small numbers:

| Bump | When |
|------|------|
| Patch (`3.7.x`) | The default for every release, including new features, capabilities and fixes. |
| Minor (`3.x.0`) | A deliberate milestone: a large redesign, or removed flags or commands that users must react to, with automatic config migration. |
| Major (`x.0.0`) | Users must hand-edit config or change scripts because aide cannot migrate it for them. |

When in doubt, choose the smaller bump. List removed or changed behaviour under "💥 Breaking changes" in the release notes whatever the bump.

To release:

1. On a branch, replace `RELEASE_NOTES.md` with only the new version, under `## vX.Y.Z (YYYY-MM-DD)`. Commit it as "Prepare release notes for vX.Y.Z" and merge the PR. goreleaser publishes this file as the top of the GitHub release.
2. Tag the merged commit and push the tag: `git tag -a vX.Y.Z -m "Release vX.Y.Z"` and `git push origin vX.Y.Z`. The `v*` tag starts `.github/workflows/release.yml`, which runs the tests and security scans, then goreleaser.

## Security

- gosec and gitleaks run in CI.
- Never commit plaintext secrets, `.env` files, or age private keys.
- Report security issues privately -- do not open a public issue.
