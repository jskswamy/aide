# Agent Sandbox Threat-Model Sweep and Design Decisions Skill

Use this skill whenever a Pull Request modifies any security-critical boundary or package in `aide`, including:
- `pkg/seatbelt/`, `internal/sandbox/`, `internal/capability/`
- `internal/gateway/`, `internal/gatewaymigrate/`, `internal/gatewaypreset/`
- `internal/secrets/`, `internal/envscrub/`, `internal/redact/`, `internal/launcher/`
- `internal/trust/`, `internal/consent/`, `internal/approvalstore/`
- `internal/provision/`, `internal/harness/`, `internal/piinstall/`
- `internal/config/`, `internal/context/`
- Or any code that emits a sandbox rule, routes proxy credentials, decrypts a secret, evaluates `.aide.yaml`, or spawns a subprocess.

---

## 1. Five Trust Boundaries of `aide`

Every security-relevant change is evaluated across the five boundaries below. The sandboxed agent must never gain access to host resources, credentials, or untrusted capabilities without explicit user consent.

1. **Boundary 1 -- OS Sandbox (`pkg/seatbelt/`, `internal/sandbox/`, `internal/capability/`):**
   - macOS Seatbelt profile generation and Linux Landlock + seccomp + bubblewrap enforcement (`DD-33`).
   - High-severity vectors (score 4-5): missing `(deny default)` baseline (`DD-27`), deny rules emitted before allow rules (`DD-26`), capability rules applied after `never_allow` (`DD-30`), mid-session permission escalation (`DD-31`), `filepath.EvalSymlinks` or `os.Getenv` operands emitted into rules without a trusted prefix or `ExistsOrUnderHome` validation check, and behavioral enforcement gaps between `_darwin.go` and `_linux.go`.
2. **Boundary 2 -- Credential-Injecting Gateway (`internal/gateway/`, `internal/gatewaymigrate/`, `internal/gatewaypreset/`):**
   - Local HTTP proxy that holds real upstream provider credentials while exposing a scoped proxy endpoint to the sandboxed agent.
   - High-severity vectors (score 4-5): real upstream API keys or tokens leaking into agent-visible environment variables, HTTP response headers/bodies, error strings, logs, or statuslines; SSRF or unvalidated upstream host routing that sends real credentials to an attacker-controlled URL; unclosed HTTP response bodies or orphaned listeners; data races on shared gateway state.
3. **Boundary 3 -- Ephemeral Secrets & Environment Scrubbing (`internal/secrets/`, `internal/envscrub/`, `internal/redact/`, `internal/launcher/`):**
   - In-process SOPS decryption (`DD-4`), `$XDG_RUNTIME_DIR/aide-<pid>/` runtime storage (`DD-10`), and environment scrubbing before process launch.
   - High-severity vectors (score 3-5): plaintext secrets written to persistent disk, runtime directories created with permissions broader than `0700` (or files broader than `0600`), missing cleanup on exit or signals (`SIGTERM`, `SIGINT`, `SIGQUIT`, `SIGHUP`), `never_allow_env` bypassed before `exec.Command`, or unredacted secret values in `explain`/`diag`/UI output.
4. **Boundary 4 -- Untrusted Project Config & Provisioning (`internal/trust/`, `internal/consent/`, `internal/approvalstore/`, `internal/provision/`, `internal/config/`):**
   - Project-local `.aide.yaml` is attacker-controlled until content-addressed SHA-256 trust verification (`DD-32`) succeeds.
   - High-severity vectors (score 4-5): evaluating capabilities, hooks, MCP servers, or environment overrides from `.aide.yaml` before SHA-256 trust verification passes; TOCTOU race between hashing and parsing `.aide.yaml`; path-based deny bypassed via content hashing; path traversal outside target profile directories during provisioning.
5. **Boundary 5 -- Subprocess & Binary Execution (`internal/launcher/`, `internal/harness/`, `internal/piinstall/`):**
   - `internal/launcher/` is the single agent execution entry point.
   - High-severity vectors (score 3-5): spawning subprocesses (`exec.Command`, `os.StartProcess`) outside `internal/launcher/` without explicit review, unvalidated `PATH` binary lookup feeding sandbox or exec paths, or discarding errors (`_ =`) on path normalization/symlink resolution in security-critical packages without an inline soundness comment.

---

## 2. Threat Scoring Scale (0-5)

| Score | Label | Meaning | Action |
|---|---|---|---|
| `0` | None | No security-relevant changes | Pass |
| `1` | Minimal | Refactor or test change in security package with identical behavior | Note in review |
| `2` | Low | Scoped allow rule or capability change with validated inputs and `never_allow` ceiling intact | Verify scope |
| `3` | Medium | Guard logic change, new env passthrough, new file access pattern, or any unvalidated input row | Flag for maintainer review |
| `4` | High | Sandbox profile generation, gateway credential routing, secret lifecycle, `.aide.yaml` trust gate, or exec path change with risk | Require fix before merge |
| `5` | Critical | Weakens `(deny default)`, bypasses `never_allow` or `.aide.yaml` trust gate, exposes real credentials to agent/disk, or allows sandbox escape | Block merge (`Must fix`) |

---

## 3. Mandatory Output: `## Threat-Model Sweep`

When triggered, the PR review MUST include this exact section (even if no vectors are found):

```markdown
## Threat-Model Sweep

Score: X/5 (<Label>)
Attack surface change: increased | unchanged | decreased

| Input | Source | Boundary (1-5) | Sink / Operand | Validated? |
|---|---|---|---|---|
| <name> | os.Getenv / .aide.yaml / EvalSymlinks / LookPath / argv / proxy req | B1..B5 | sandbox rule / exec.Command / sops / upstream req | yes/no -- file:line citation |

Vectors:
| # | file:line | Vector | Mitigated in diff? |
|---|---|---|---|
| V1 | path/file.go:42 | <concrete exploit or failure path> | yes/no |

Score derivation: <one sentence per row above 2/5>
```

**Rules for filling the table:**
- Enumerate every `os.Getenv`, `EnvLookup`, `exec.LookPath`, `filepath.EvalSymlinks`, `argv[]`, `.aide.yaml` field, or proxy request header/URL whose result reaches a sandbox rule operand, secret decryption call, upstream proxy request, or `exec.Command`.
- `Validated?` is `yes` only when the diff or a called helper constrains the input (allowlist prefix check, `ExistsOrUnderHome`, SHA-256 trust gate check, `never_allow` ceiling cited by `file:line`, or deferred deny rule that shadows the operand).
- Any `Validated? no` row forces the score to at least `3/5` and requires a row in `Vectors` and a `Must fix` finding.

---

## 4. Load-Bearing Design Decisions (`DD-1` through `DD-33`)

Flag any violation of these decisions from `DESIGN.md` as a `Must fix` finding unless the PR explicitly updates `DESIGN.md` with rationale:

- **Sandbox (`DD-14, DD-15, DD-22, DD-23, DD-26..DD-31, DD-33`):**
  - `DD-14`: Sandbox is on by default (opt-out only, never opt-in).
  - `DD-15`: Guards-based default policy (`always` guards cannot be disabled; `default` guards are on by default).
  - `DD-22`: Agent config directories use resolver functions, never hardcoded paths.
  - `DD-23`: Non-existent literal paths skip with a warning at launch; globs pass through.
  - `DD-26`: Deny-wins semantics -- use allow-broad + deny-narrow; deny rules emit after allow rules.
  - `DD-27`: Seatbelt profiles start with `(deny default)`. Never emit `(allow default)`.
  - `DD-28`: All sandbox access decisions go through guards.
  - `DD-29`: Capabilities resolve to sandbox fields (`writable`, `readable`, `denied`, `env_allow`) on top of guards.
  - `DD-30`: `never_allow` appends last after base + capabilities + explicit config. Nothing overrides it.
  - `DD-31`: No mid-session permission escalation; sandbox policy is immutable after launch.
  - `DD-33`: Linux sandbox enforces Landlock + seccomp + bubblewrap fallback with parity against Darwin policy semantics.
- **Secrets & Environment (`DD-4, DD-6, DD-10, DD-11, DD-13, DD-17`):**
  - `DD-4`: Decrypt in-process via `sops/v3/decrypt` Go library; never shell out to `sops` CLI at runtime.
  - `DD-6`: Single XDG config directory (`$XDG_CONFIG_HOME/aide/`).
  - `DD-10`: Ephemeral runtime files live in `$XDG_RUNTIME_DIR/aide-<pid>/` (`0700`) and are wiped on exit and signals.
  - `DD-11`: Secrets are optional; literal env values without `{{ }}` pass through.
  - `DD-13`: Age key discovery order: YubiKey -> `$SOPS_AGE_KEY` -> `$SOPS_AGE_KEY_FILE` -> default key file.
  - `DD-17`: Inherit shell environment by default; `--clean-env` is opt-in isolation.
- **Profiles, Contexts, & Trust (`DD-5, DD-7, DD-12, DD-16, DD-18, DD-24, DD-32`):**
  - `DD-5`: Env, secrets, and MCP selection belong on contexts/profiles, not agent binary definitions.
  - `DD-7` & `DD-24`: Built-in default profile fallback when no custom profile is configured.
  - `DD-12`: Minimal flat config format remains supported.
  - `DD-16`: All CLI arguments after `aide`'s flags forward directly to the agent.
  - `DD-18`: Project root resolves by walking up to `.git/` (falling back to `cwd`).
  - `DD-32`: `.aide.yaml` is untrusted by default, gated by content-addressed SHA-256 approval, with path-based deny.
