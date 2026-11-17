# Documentation Sync and Accuracy Audit Skill

Use this skill whenever a Pull Request changes CLI commands/flags, configuration schema, capabilities, sandbox guards, gateway routing, provisioning, secrets, or architectural boundaries, OR modifies any Markdown file in `docs/`, `README.md`, or `DESIGN.md`.

Report documentation drift or inaccuracies under `### Should fix` in the Ponytail review output.

---

## 1. Code-to-Documentation Sync Matrix

When a PR changes behavior or public contracts in a source path below, verify that the corresponding documentation file is updated in the **same PR**:

| Changed Source Path / Contract | Required Documentation Update |
|---|---|
| `cmd/aide/` (new/modified CLI command, subcommand, flag name, flag type, default value, or argument count) | `docs/cli-reference.md` (and `README.md` or `docs/getting-started.md` if top-level UX changed) |
| `internal/config/` (new/modified YAML struct tag, `.aide.yaml` field, profile schema, or validation rule) | `docs/configuration.md` and/or `docs/profiles.md` |
| `internal/capability/` (new/modified built-in capability, `extends`/`combines`, or capability effect) | `docs/capabilities.md` |
| `internal/sandbox/` or `pkg/seatbelt/` (new/modified guard, default policy, Linux Landlock/seccomp/bwrap behavior, or Seatbelt public API) | `docs/sandbox.md` (and `DESIGN.md` if `DD-*` invariants change) |
| `internal/gateway/`, `internal/gatewaymigrate/`, `internal/gatewaypreset/` (proxy routing, auth flags, provider presets, or credential migration) | `docs/designs/gateway-redesign.md`, `docs/configuration.md`, or `docs/cli-reference.md` |
| `internal/provision/`, `internal/harness/`, `internal/piinstall/` (agent provisioning, skillshare sync, or isolated harness layout) | `docs/provisioning.md`, `docs/skillshare.md`, or `docs/profiles.md` |
| `internal/secrets/`, `internal/envscrub/` (secret resolution, SOPS/Age key order, template variables, or `--clean-env` / `never_allow_env`) | `docs/secrets.md` and/or `docs/environment.md` |
| Architectural boundaries, new `internal/` or `pkg/` packages, or changes to `DD-1` through `DD-33` | `DESIGN.md` |

*(Internal refactors, bug fixes that restore already-documented behavior, and test-only changes do not require documentation updates.)*

---

## 2. Factual Accuracy Audit on Modified Docs

When a PR adds or edits documentation in `README.md`, `DESIGN.md`, `CONTRIBUTING.md`, `AGENTS.md`, or `docs/*.md`, cross-check every factual claim against the working tree:

1. **Commands and Flags:**
   - Every `aide <command>` and `--flag` shown in prose or code blocks must exist in `cmd/aide/` with matching flag type, default value, and positional argument rules.
2. **Configuration Keys and Templates:**
   - Every YAML key shown in examples must match a struct field's `yaml:"..."` tag in `internal/config/`.
   - Every template variable (`{{ .secrets.* }}`, `{{ .project_root }}`, `{{ .runtime_dir }}`) must match supported template bindings.
3. **Code Symbol and Claim Verification:**
   - Every type, interface, function, file path, or CLI flag named in the PR description or documentation must exist in the codebase at the reviewed SHA. Hallucinated or stale symbol names are `Should fix` findings.
4. **Classification of Doc Defects:**
   - **MISSING:** Implemented user-facing feature, flag, config field, or capability is not documented in its target reference file.
   - **WRONG:** Documented command, flag, config key, or default value contradicts the code.
   - **STALE:** Code behavior changed in this PR (or an earlier commit) while the adjacent documentation still describes the old behavior.
   - **MISLEADING:** Prose implies guarantees the code does not provide (for example, claiming a file is encrypted when it is plaintext, or claiming a flag applies to all harnesses when it only applies to one).
