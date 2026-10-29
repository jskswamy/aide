# Critic & Threat Modeling Report (Stage 7): `docs/designs/gateway/design.md`

**Audited Scope:** `FeatureRange: 1..507` (cross-checked with `DetailedRange: 508..765`)
**Standards Applied:** `feature-spec-standard.md` + Dedicated Threat Modeling Audit (Design Critic, Document Architect, Style Reviewer)

---

## Part 1: Design, Structural & Style Findings

### 🔴 Blocker 1: Unresolved Multi-Gateway & Existing Default Collision in `default_credentials` (`checkDefaultRoutes`)
* **Affected Scope:** [lines 106–125, 140–141, 524–558](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L106-L125) and [`checkDefaultRoutes`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L190-L214).
* **Finding:** `checkDefaultRoutes(ctx)` forbids two bindings in `ctx.DefaultCredentials` from setting the same environment variable (`r.Env` or `r.Set`) for the same harness. If two gateways both target `claude` (or a gateway is added alongside `default_credentials: [anthropic]`), auto-appending both to `DefaultCredentials` will fail `checkDefaultRoutes` on config load.
* **Recommendation:** Only auto-append a synthesized gateway to `DefaultCredentials` when no existing binding in `DefaultCredentials` sets an overlapping env/`set:` variable on the gateway's target harnesses (and iterate `ctx.Gateways` in sorted key order so the first gateway alphabetically becomes default unless overridden by `default_credentials`).

### 🔴 Blocker 2: `DeleteKey` on a Single-Key Secret Store Fails `EditFromContent` Validation
* **Affected Scope:** [lines 414–424, 574–585](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L574-L585) and [`Manager.EditFromContent`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/secrets/manager.go#L152-L196).
* **Finding:** When `aide gateway remove` deletes the last remaining key in `<store>.enc.yaml`, `data` is empty (`{}`). Passing an empty map to `EditFromContent` fails SOPS encryption/validation.
* **Recommendation:** If `len(data) == 0` after deleting `key`, `DeleteKey` must remove `<storeName>.enc.yaml` (`os.Remove(srcPath)`) rather than calling `EditFromContent`.

### 🔴 Blocker 3: Contradictory HTTP Timeout, Retry, and Endpoint Path Specifications
* **Affected Scope:** [line 180](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L180) vs. [line 498](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L498) vs. [lines 636–643](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L636-L643).
* **Recommendation:** Standardize on `GET <NormalizeBaseURL(baseURL)>/v1/models?limit=1000` with a `10s` timeout and `4 MiB` `io.LimitReader`, and store `NormalizeBaseURL(url)` in `config.yaml` so `opencode`/`codex` appending `/v1` never produces `/v1/v1`.

### 🟡 Major 1: Codex Model Catalogue / Provider Selection & Claude Multi-Gateway Behavior
* **Affected Scope:** [lines 216–270, 444–450](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L216-L270).
* **Recommendation:** Specify how `InspectHarness` evaluates `synced` vs `stale` for `codex`, and specify that during a multi-gateway `aide gateway sync` only the active/default `claude` gateway writes `<claude-dir>/cache/gateway-models.json`.

### 🟡 Major 2: Inconsistent Launch-Time Warning Prefix and Message Format on Refresh Failure
* **Affected Scope:** [lines 440–443](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L440-L443) vs. [line 499](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L499).
* **Recommendation:** Unify line 499 with Section 7 (`aide: warning: could not refresh gateway "<name>" (<err>); using cached catalogue from <age> ago (<N> models)`).

---

## Part 2: Dedicated Threat Modeling Audit

### 1. New Threat Vectors & Required Mitigations

#### 🔴 Threat Blocker 1: Cleartext Bearer Token Transmission via `http://` and Cross-Host Redirect Token Leakage
* **Vector:** Transport Security (`internal/gateway/fetch.go` & `internal/config/gateway.go`, [lines 155, 180, 636–643](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L155))
* **Risk:**
  1. Allowing `http://` on non-loopback hosts transmits the gateway bearer token in cleartext over the network on every sync/launch.
  2. Default `http.Client` redirect handling can follow `https://` -> `http://` scheme downgrades or cross-host redirects.
  3. Unchecked `u.User`, `u.RawQuery`, or `u.Fragment` in `base_url` can smuggle credentials into `config.yaml` or corrupt `/v1/models` URL construction.
* **Mitigation:**
  * Reject `http://` in `ValidateGateways` and `aide gateway add` unless the host is loopback (`localhost`, `127.0.0.1`, `::1`).
  * Configure `http.Client.CheckRedirect` in `FetchModels` to reject cross-origin redirects and `https` -> `http` downgrades.
  * Reject `base_url` values with `u.User != nil`, `u.RawQuery != ""`, or `u.Fragment != ""`.

#### 🟡 Threat Major 1: Untrusted `/v1/models` Payload Attacks (Terminal Escape Injection & Resource Exhaustion)
* **Vector:** Untrusted Remote Input ([lines 181–186, 216–270, 367–372](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L181-L186))
* **Risk:** A compromised or shared upstream gateway returning ANSI/OSC escape sequences in `id`, `display_name`, or `provider` can spoof terminal output or manipulate the clipboard when `aide gateway list --models` or `aide gateway sync` prints to the terminal; unbounded object counts in a `4 MiB` JSON array can bloat harness config files.
* **Mitigation:** Strip/reject control characters (`r < 0x20 || r == 0x7f` and C1 controls `0x80..0x9f`) in `ID`, `DisplayName`, and `Provider`, cap field lengths (`<= 256` bytes), and cap normalized models at `1000`.

#### 🟡 Threat Major 2: Environment Variable Collision & Injection via `--key` (`strings.ToUpper(key)`)
* **Vector:** Environment Variable Injection ([lines 82, 157, 306](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L157))
* **Risk:** `secrets/manager.go` allows `-` in secret key names, which produces invalid env var names if hand-edited in `config.yaml`; additionally, `reservedEssentialEnv` does not block `LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`, `NODE_OPTIONS`, `HTTP_PROXY`, `HTTPS_PROXY`, etc., or `cfg.NeverAllowEnv`.
* **Mitigation:** Validate `gw.Key` against `^[a-zA-Z_][a-zA-Z0-9_]*$`, check `gw.EnvVarName()` against `checkBindingEnv`, `cfg.NeverAllowEnv`, and loader/proxy env vars (`LD_*`, `DYLD_*`, `NODE_OPTIONS`, `HTTP_PROXY`, `HTTPS_PROXY`).

#### 🟡 Threat Major 3: Unvalidated `profile` and `gatewayName` in `CachePath` (Path Traversal)
* **Vector:** Local Filesystem Path Traversal ([lines 154–160, 188, 645–654](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L188))
* **Risk:** Hand-edited `config.yaml` with `..` or `/` in gateway or profile names could escape `~/.local/state/aide/gateways/`.
* **Mitigation:** Validate gateway `<name>` and `store` against `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` in `ValidateGateways` and reject path separators/`..` in `CachePath`.

#### 🟡 Threat Major 4: Plaintext Secret Temp File in `Manager.EditFromContent` & Cross-Profile Secret Deletion
* **Vector:** Local Secret Material Exposure ([lines 163–176, 412, 565–585](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L163-L176))
* **Risk:** `Manager.EditFromContent` currently writes decrypted plaintext secrets to a temp file in `os.TempDir()` even though it never launches an editor; and `aide gateway remove` must check across **all profiles** in `cfg.Contexts` before deleting `<key>` from `<store>.enc.yaml`.
* **Mitigation:** Encrypt in memory in `SetKey` / `DeleteKey` (and remove the dead temp-file write in `EditFromContent`), and check all profiles in `cfg.Contexts` for references to `(store, key)` before calling `DeleteKey`.

---

### 2. How `aide gateway` Improves `aide`'s Security Posture vs. Status Quo

1. **Eliminates Plaintext Token Sprawl in Dotfiles**: Replaces raw `"apiKey": "sk-..."` strings in `~/.pi/agent/models.json` and `~/.config/opencode/opencode.jsonc` with symbolic env var references backed by `age`-encrypted secret stores.
2. **Eliminates Process Table (`ps`), Shell History, and `$EDITOR` Swap-File Leaks**: Uses `term.ReadPassword`, `--token-stdin`, or `--token-env <VAR>` and programmatic in-memory encryption instead of CLI token arguments or `$EDITOR` temp files.
3. **Automatic Cross-Profile Environment Scrubbing**: Registers `gw.EnvVarName()` and `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY` in `CredentialScrubNames()` / `providerSelectorNames` so ambient shell variables cannot leak across profiles.
4. **Upstream `/v1/models` Payload Sanitization**: Projects only the 5 allowlisted display fields (`id`, `display_name`, `provider`, `context_window`, `max_output_tokens`) into the local cache.
5. **Untrusted Repository Protection (`.aide.yaml` Isolation)**: Blocks `gateways:` in `.aide.yaml` via `projectDefiningKeys` and routes `ANTHROPIC_BASE_URL` inside the credential binding so untrusted repos cannot redirect gateway tokens.
