# Consolidated EGM Goldfish Review & Threat Modeling Report

**Target Document:** [`docs/designs/gateway/design.md`](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md) (`FeatureRange: 1..507`, `DetailedRange: 508..765`)
**Stage Reports:**
* Stage 5: [zoom_out_report.md](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/zoom_out_report.md)
* Stage 6: [comprehension_report.md](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/comprehension_report.md)
* Stage 7: [critic_report.md](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/critic_report.md)
* Stage 8: [intermediate_zoom_out_report.md](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/intermediate_zoom_out_report.md)
* Stage 9: [implementation_readiness_report.md](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/implementation_readiness_report.md)

---

## Part 1: Dedicated Threat Modeling Assessment

### A. How `aide gateway` Improves `aide`'s Security Posture vs. Status Quo

Compared to the status quo (manual `credentials:` setup combined with external `curl`/`jq` scripts), `aide gateway` delivers five concrete security improvements:

1. **Eliminates Plaintext Token Sprawl Across Harness Config Files**:
   * *Before*: Users configuring Pi (`~/.pi/agent/models.json`) or OpenCode (`~/.config/opencode/opencode.jsonc`) by hand frequently paste raw bearer tokens (`sk-...`) directly into `"apiKey"` fields in unencrypted dotfiles.
   * *After*: Harness config files contain only symbolic environment variable references (`"apiKey": "CORP_TOKEN"`, `"apiKey": "{env:CORP_TOKEN}"`, `env_key = "CORP_TOKEN"`). The bearer token remains encrypted at rest in `~/.config/aide/secrets/<store>.enc.yaml` and is injected only into the launched agent process environment.
2. **Eliminates Process Table (`ps`), Shell History, and `$EDITOR` Swap-File Exposure**:
   * *Before*: Users either exported tokens in `.zshrc` (exposing them to all user processes) or ran `aide secrets edit` in `$EDITOR` (risking editor swap/undo files or terminal scrollback exposure).
   * *After*: `aide gateway add` reads tokens via hidden TTY prompt (`term.ReadPassword`), `--token-stdin`, or `--token-env <VAR>` (passing only the variable name, never the secret value on the command line) and encrypts in memory via `SetKey`.
3. **Automatic Cross-Profile Environment Scrubbing**:
   * *Before*: Custom gateway token variables (such as `CORP_TOKEN`) exported in a user's shell are unknown to `internal/envscrub`'s static `providerSecretNames` list and leak into every agent session across all profiles unless manually listed in `scrub_env:`.
   * *After*: `(*Config).CredentialScrubNames()` automatically registers `gw.EnvVarName()` (`CORP_TOKEN`) and `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY` in the scrub registry so ambient shell exports cannot leak into other profiles.
4. **Upstream `/v1/models` Payload Sanitization**:
   * *Before*: Ad-hoc `curl /v1/models > gateway-models.json` scripts write raw upstream JSON directly to disk, including any internal metadata returned by proxy/LiteLLM deployments.
   * *After*: `FetchModels` projects only the 5 allowlisted display fields (`id`, `display_name`, `provider`, `context_window`, `max_output_tokens`) into `CachedCatalogue`.
5. **Untrusted Repository Protection (`.aide.yaml` Isolation)**:
   * *After*: Appending `"gateways"` to `projectDefiningKeys` in [`internal/config/profiles.go`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/profiles.go#L47-L51) and routing `ANTHROPIC_BASE_URL` through the synthesized credential route's `Set` map ensures [`checkEndpointsRouted`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/launcher/credentials.go#L277-L300) blocks any repository `.aide.yaml` from redirecting a profile's gateway token to an untrusted host.

---

### B. New Threat Vectors Identified & Required Design Hardening

| ID | Severity | Threat Vector | Attack Scenario | Required Mitigation in `design.md` |
| :--- | :--- | :--- | :--- | :--- |
| **TM-1** | 🔴 Blocker | **Cleartext `http://` & Redirect Token Leakage** (`internal/gateway/fetch.go`, `internal/config/gateway.go`) | 1. Allowing `http://` on remote hosts transmits `Authorization: Bearer <token>` in cleartext.<br>2. Default `http.Client` follows up to 10 redirects, allowing a compromised gateway or MITM to downgrade `https` $\rightarrow$ `http` or redirect to internal metadata endpoints.<br>3. `base_url` with `user:pass@`, `?query`, or `#fragment` smuggles credentials or corrupts `/v1/models` URL construction. | 1. Require `https://` in `ValidateGateways` and `aide gateway add` unless the host is loopback (`localhost`, `127.0.0.1`, `::1`).<br>2. Configure `http.Client.CheckRedirect` in `FetchModels` to reject cross-origin redirects and `https` $\rightarrow$ `http` downgrades.<br>3. Reject `base_url` with `u.User != nil`, `u.RawQuery != ""`, or `u.Fragment != ""`. |
| **TM-2** | 🟡 Major | **Untrusted `/v1/models` Payload Attacks (ANSI Escape & Resource Exhaustion)** (`internal/gateway/fetch.go`) | A compromised or shared gateway returning ANSI/OSC escape sequences (`\x1b]52;...`) in `id`, `display_name`, or `provider` can spoof terminal output or write to the clipboard when `aide gateway list --models` or `sync` prints to stdout/stderr; a `4 MiB` JSON array of tiny objects can create ~250k models. | Strip/reject control characters (`r < 0x20 \|\| r == 0x7f` and `0x80..0x9f`) in `ID`, `DisplayName`, and `Provider`; cap string lengths (`<= 256` bytes) and cap normalized models at `1000`. |
| **TM-3** | 🟡 Major | **Environment Variable Injection via `gw.Key` $\rightarrow$ `strings.ToUpper(key)`** (`internal/config/gateway.go`) | `secrets/manager.go` permits `-` in secret keys, which is invalid in env var names if hand-edited in `config.yaml`; `reservedEssentialEnv` does not block `LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`, `NODE_OPTIONS`, `HTTP_PROXY`, `HTTPS_PROXY`, or `cfg.NeverAllowEnv`. | Validate `gw.Key` against `^[a-zA-Z_][a-zA-Z0-9_]*$`, and reject `gw.EnvVarName()` if it matches `checkBindingEnv`, `cfg.NeverAllowEnv`, or dangerous loader/proxy prefixes (`LD_*`, `DYLD_*`, `NODE_OPTIONS`, `HTTP_PROXY`, `HTTPS_PROXY`). |
| **TM-4** | 🟡 Major | **Path Traversal via Unvalidated `profile` / `gatewayName` in `CachePath`** (`internal/gateway/cache.go`) | Hand-editing `config.yaml` with `..` or `/` in gateway or profile names could escape `~/.local/state/aide/gateways/`. | Validate gateway `<name>` and `store` against `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` in `ValidateGateways`, and reject path separators or `..` in `CachePath`. |
| **TM-5** | 🟡 Major | **Plaintext Temp File in `EditFromContent` & Cross-Profile Secret Deletion** (`internal/secrets/manager.go`) | `Manager.EditFromContent` currently writes decrypted plaintext secrets to a temp file in `os.TempDir()` even though no editor is launched; `aide gateway remove` deleting `<key>` without checking other profiles could break another profile sharing `<store>.enc.yaml`. | Encrypt in memory in `SetKey`/`DeleteKey` without writing plaintext temp files to `/tmp`, and check all profiles in `cfg.Contexts` before deleting `<key>` on `aide gateway remove`. |

---

## Part 2: Consolidated Actionable Findings (All 5 Review Stages)

### 🔴 Blockers

1. **C-B1: Align `SynthesizeGatewayCredentials` with Existing Codebase Symbols, Pure Value Pattern, and `checkDefaultRoutes` Invariants**
   * *Stages*: Stage 5 (#1), Stage 6 (Blocker 2), Stage 7 (Blocker 1), Stage 9 (Blockers 1, 2, 6)
   * *Details*:
     * `ValidateCredentials` and `ResolveCredentials` do not exist in `internal/config/credentials.go`; the actual methods are `(*Config).validateCredentials()` in `internal/config/credentials.go` and `profileCredentials()` / `PlanProfileCredentials()` in `internal/launcher/credentials.go`.
     * `CredentialScrubNames()` has receiver `(c *Config)`, not `(c Context)`.
     * `projectDefiningKeys` in `internal/config/profiles.go` is a `[]string` slice, not `map[string]bool`.
     * Follow [`TranslateLegacyCredentials(ctx Context, store string) (Context, []LegacyCredential, error)`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L389): make `SynthesizeGatewayCredentials(ctx Context) (Context, error)` return a cloned `Context` with `Backend: CredentialBackendAge` on each synthesized `CredentialBinding`, eliminating the need for `synthesizedCreds` / `synthesizedDefaultCreds` shadow maps on `config.Context`.
     * Specify deterministic gateway ordering (`slices.Sorted(maps.Keys(ctx.Gateways))`), reject gateway names that collide with explicit `ctx.Credentials[<name>]`, and only auto-append a gateway to `DefaultCredentials` for harnesses where no existing binding in `DefaultCredentials` sets an overlapping env/`set:` variable (preventing `checkDefaultRoutes` failures).

2. **C-B2: Reconcile `/v1/models` HTTP Timeout, Redirect Policy, and `NormalizeBaseURL` Contract Across Sections**
   * *Stages*: Stage 5 (#2), Stage 6 (Blocker 1), Stage 7 (Blocker 3 & Threat Blocker 1), Stage 9 (#7)
   * *Details*:
     * Line 180 (`20s` timeout + 3 retries with backoff) contradicts line 498 (`10s` `context.WithTimeout` + `4 MiB` `io.LimitReader`) and risks stalling interactive `aide` launches for 60+ seconds when offline.
     * Standardize on a single request to `GET <NormalizeBaseURL(baseURL)>/v1/models?limit=1000` with a `10s` timeout (`5s` on launch-time refresh when a fallback cache exists), `4 MiB` `io.LimitReader`, `https://` required for non-loopback hosts, and cross-origin/scheme-downgrade redirects blocked in `http.Client.CheckRedirect`.
     * Apply `NormalizeBaseURL` when saving `base_url` to `config.yaml` so `claude`/`pi` use `NormalizeBaseURL(gw.BaseURL)` and `opencode`/`codex` use `NormalizeBaseURL(gw.BaseURL) + "/v1"` without ever producing `/v1/v1`.

3. **C-B3: Fix `Launcher.Launch` Types (`*aidectx.ResolvedContext`), Ordering, and Cold-Launch Error Handling**
   * *Stages*: Stage 6 (Blocker 3), Stage 7 (Major 2), Stage 9 (Blocker 3)
   * *Details*:
     * Replace non-existent `rc *config.ResolvedConfig`, `rc.ProfileName`, and `ensureProfileDirProvisioned` with the actual `Launcher.Launch` types: `rc *aidectx.ResolvedContext`, `rc.Name`, `credPlan CredentialPlan`, and `homeDir string`.
     * Specify that `ensureGatewaySynced` runs in `Launcher.Launch` immediately after `ResolveCredentialEnv(credPlan, loadStore)` (and after first-launch profile directory checks), is skipped under `--auth login` (when `credPlan.Bindings` is empty), and keeps `aide which` / `aide status` side-effect-free.

4. **C-B4: Fix `statedir.Path(homeDir, name)` Signature and Pass `homeDir` Through `internal/gateway/cache.go`**
   * *Stages*: Stage 5 (#3), Stage 7 (Threat Major 3), Stage 9 (Blocker 4)
   * *Details*:
     * `statedir.Path(homeDir, name string) string` takes two arguments (`homeDir` and `name`). Update `CachePath`, `ReadCache`, `WriteCache`, and `RemoveCache` in `internal/gateway/cache.go` to take `homeDir string` and call `statedir.Path(homeDir, filepath.Join("gateways", profile, gatewayName+".json"))`.

5. **C-B5: Fix `internal/secrets` File References (`sops.go`), `AgeIdentity.Recipient()`, Default Age Key Path, and Single-Key `DeleteKey`**
   * *Stages*: Stage 5 (#3), Stage 7 (Blocker 2 & Threat Major 4), Stage 9 (Blocker 5)
   * *Details*:
     * Point `DecryptSecretsFile` link to `internal/secrets/sops.go` (not non-existent `decrypt.go`), fix line 489 `.yaml` to `.enc.yaml`, and fix line 307 to reference `secrets.DiscoverAgeKey()` (`sops/age/keys.txt`, `SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE`).
     * Add `func (id *AgeIdentity) Recipient() (string, error)` on `AgeIdentity` in `internal/secrets/age.go`.
     * Specify in `Manager.DeleteKey` that if deleting `key` leaves `len(data) == 0`, `DeleteKey` removes `<storeName>.enc.yaml` (`os.Remove(srcPath)`) instead of passing an empty map to `EditFromContent`.

---

### 🟡 Majors

6. **C-M1: Define `InspectHarness` (`synced` / `stale` / `pending`) Predicates, Codex Model/Provider Handling, and Claude Multi-Gateway Behavior**
   * *Stages*: Stage 6 (Major 1 & Major 3), Stage 7 (Major 1), Stage 9 (Major 8)
   * *Details*:
     * Define exact predicates for `StatePending` (harness config dir does not exist), `StateStale` (config file missing, provider entry missing, `baseUrl`/`env_key` mismatch, or model ID list differs from `cat.Models`), and `StateSynced`.
     * Specify how Claude's singleton `<claude-dir>/cache/gateway-models.json` is handled when multiple gateways exist on a profile (written by the default/selected gateway for `claude`), how fallback model descriptions are formatted when `Provider` or `ContextWindow` is zero/empty, and reuse `fsutil.AtomicWrite` and `launchConfigDir` logic.

7. **C-M2: Define `aide gateway list --format json` Output Schema and Unify Warning Strings**
   * *Stages*: Stage 6 (Major 2), Stage 7 (Structural Major 2)
   * *Details*:
     * Add the JSON output struct definition (`GatewayListOutput`) for `aide gateway list --format json` (with and without `--models`).
     * Unify the launch-time refresh failure warning string between Section 7 (line 442) and Non-Functional Requirements (line 499).

8. **C-M3: Apply Threat Modeling Mitigations (TM-2, TM-3, TM-4, TM-5)**
   * *Stages*: Stage 7 (Threat Majors 1–4)
   * *Details*:
     * Control-character sanitization and count/length caps on `/v1/models` responses (TM-2).
     * Strict regex `^[a-zA-Z_][a-zA-Z0-9_]*$` and loader/proxy env var blocklist on `gw.Key` (TM-3).
     * Gateway name and store name regex validation in `ValidateGateways` and `CachePath` (TM-4).
     * In-memory encryption in `SetKey`/`DeleteKey` without `/tmp` plaintext files, and cross-profile reference check before deleting a secret key on `aide gateway remove` (TM-5).

---

### 🟢 Nits

9. **C-N1: Codebase CLI & Formatting Conventions**
   * *Stages*: Stage 5 (#4, #5), Stage 7 (Structural Nits 3–7), Stage 9 (Nits 9, 10)
   * *Details*:
     * Use `gatewayCmd()`, `gatewayAddCmd()`, `gatewayListCmd()`, `gatewayRemoveCmd()`, `gatewaySyncCmd()` constructor names in `cmd/aide/gateway.go`, and use `output.FromCmd(cmd)` / `output.Emit` for `--format`.
     * Replace `aide --profile work --agent pi --dry-run` in Manual Verification with `aide which --profile work --agent pi`.
     * Clarify `opencode.jsonc` comment handling (`hujson.Standardize` accepts comments on read and preserves sibling keys on write).
     * Remove literal `\n` escapes in Mermaid flowchart node labels and standardize `---` horizontal rules.

---

## Part 3: Phase 2 Goldfish Review Findings (`aide gateway use`, Per-Harness Defaults, Startup Banner & `aide which`)

### 🔴 Blockers (Phase 2)

1. **C-B6: `checkDefaultRoutes`, `BuiltinCredentialHarnesses`, and Mutually Exclusive Per-Harness Default Slots**
   * *Stages*: `egm-codebase-details` (Blockers 1–3), `egm-design` (Blocker 1), `egm-zoom-out` (#2), `egm-comprehension` (Blockers 1–2)
   * *Details*:
     * Calling `checkDefaultRoutes(ctx)` on the un-synthesized `ctx` skips gateway names explicitly listed in `ctx.DefaultCredentials`; instead construct `explicitCtx := synthCtx; explicitCtx.DefaultCredentials = ctx.DefaultCredentials` and call `checkDefaultRoutes(explicitCtx)`.
     * Define `BuiltinCredentialHarnesses(name string) []string` (`"anthropic"` $\rightarrow$ `["claude", "pi", "opencode"]`, `"openai"` $\rightarrow$ `["pi", "opencode", "codex"]`, `"codex"` $\rightarrow$ `["codex"]`, `"openrouter"` $\rightarrow$ `["pi", "opencode", "codex"]`, `"gemini"` $\rightarrow$ `["gemini", "pi", "opencode"]`) and enforce at most one default LLM provider/gateway binding per harness in `DefaultCredentialsForHarness(synthCtx, harnessName)` so `anthropic` and `api` do not both inject tokens on `pi`/`opencode` and `anthropic` does not claim `codex`.
     * Reference the actual credential selection functions `PlanProfileCredentials`, `(*Selection).SelectCredentials`, and `(*Selection).PlanCredentials`.

2. **C-B7: Preserve Existing `compact`, `clean`, and `boxed` Banner Template Contracts and Use Real Capability Resolution in `whichCmd`**
   * *Stages*: `egm-design` (Blockers 2–3), `egm-codebase-details` (Blocker 5, Majors 7–8), `egm-structural` (Majors 3–4), `egm-zoom-out` (#4)
   * *Details*:
     * Existing unit tests in `internal/ui/banner_test.go` assert on `secret:`, `env:`, `sandbox:`, `code-only`, per-capability security path lines (`✓ <name>[<variant>]`), `isolationTierLabel`, and `AUTO-APPROVE` across `compact.tmpl`, `clean.tmpl`, and `boxed.tmpl`. Keep those structural lines intact while adding `HarnessConfigDir`, `AvailableHarnesses`, `AvailableGateways`, and enriched `CredentialInfo` (`URL`, `Store`, `Key`, `IsGateway`, `ModelCount`, `ModelAge`, `TargetFile`, `TargetSync`).
     * In `whichCmd` (`cmd/aide/status.go`), resolve `agentName` via `cfg.Agents[agentName].Binary`, strip `envscrub.ConfigDirRedirects()`, and resolve capabilities via `sandbox.MergeCapNames`, `capability.AutoInclude`, `sandbox.ResolveCapabilitiesWithVariants(..., sandbox.VariantSelectionOptions{Interactive: false, ...})`, and `sandbox.ApplyOverrides(&resolvedSandbox, capOverrides)`.

---

### 🟡 Majors (Phase 2)

3. **C-M4: Deterministic `CanSyncHarness` for Claude & `codex` Materialization on `aide gateway use`**
   * *Stages*: `egm-design` (Majors 1, 3), `egm-zoom-out` (#1), `egm-codebase-details` (Majors 4, 6), `egm-comprehension` (Majors 1–2)
   * *Details*:
     * When no custom gateway in `ctx.Gateways` is Claude's default (`IsDefaultGateway(ctx, g, "claude") == false`), `CanSyncHarness(ctx, gwName, "claude")` returns `true` only for the first custom gateway in `slices.Sorted(maps.Keys(ctx.Gateways))` that supports `claude`, preventing two non-default custom gateways from overwriting `cache/gateway-models.json`. In `InspectHarness`, report `StatePending` (`"not claude's active gateway; syncs when selected"`) when `!CanSyncHarness(ctx, gwName, "claude")`.
     * Update `codexFile.write` so `model_provider = gwName` is updated when `isDefault` is `true` and `model_provider` is unset or set to another gateway in `ctx.Gateways`, and materialize both `claude` and `codex` in `aide gateway use`.
     * Add `EnvVar string` to `HarnessFileStatus` and update `RemoveFromHarness` signature to `(bool, error)`.

4. **C-M5: Interactive TTY Prompt Guards & Shared `bufio.Reader` in `aide gateway add`**
   * *Stages*: `egm-design` (Major 2), `egm-zoom-out` (#3), `egm-comprehension` (Major 3), `egm-structural` (Majors 1–2)
   * *Details*:
     * Use a single shared `bufio.NewReader(cmd.InOrStdin())` across interactive prompts in `gatewayAddCmd` and guard `--url`, `<name>`, and `--default` prompts with `!tokenStdin && tokenEnv == "" && stdinIsTTY()`, only prompting `Gateway name [<derived>]: ` when `<name>` was omitted and `--url` was also prompted (or when the derived hostname label is invalid).

---

## Part 4: Traceability Matrix

| Consolidated ID | Priority | Source Stage Reports & Finding IDs | Target Sections in `design.md` |
| :--- | :--- | :--- | :--- |
| **C-B1** | 🔴 Blocker | Stage 5 (#1), Stage 6 (B2), Stage 7 (B1, M3), Stage 9 (B1, B2, B6) | §2 (`Configuration Schema`), §6.2, Detailed Plan §1 |
| **C-B2 (incl. TM-1)** | 🔴 Blocker | Stage 5 (#2), Stage 6 (B1), Stage 7 (B3, TM-B1, Structural M1), Stage 9 (M7) | §2, §4 (`Fetching & Normalizing`), NFRs (`Security`, `Performance`), Detailed Plan §3 |
| **C-B3** | 🔴 Blocker | Stage 6 (B3), Stage 7 (M2), Stage 9 (B3) | §7 (`Launch-Time TTL Refresh`), Detailed Plan §4 |
| **C-B4** | 🔴 Blocker | Stage 5 (#3), Stage 7 (TM-M3), Stage 9 (B4) | §4 (`Local Cache File`), Detailed Plan §3 (`cache.go`) |
| **C-B5** | 🔴 Blocker | Stage 5 (#3), Stage 7 (B2, TM-M4), Stage 9 (B5) | §3 (`Programmatic Secret Store Upsert`), §6.1, §6.4, Detailed Plan §2 |
| **C-B6** | 🔴 Blocker | Phase 2 Stage 5 (#2), Stage 6 (B1–B2), Stage 7 (B1), Stage 9 (B1–B3) | §2, §6.2, §6.3, Detailed Plan §1 |
| **C-B7** | 🔴 Blocker | Phase 2 Stage 5 (#4), Stage 7 (B2–B3), Stage 8 (M3–M4), Stage 9 (B5, M7–M8) | §8 (`Startup Banner & aide which Redesign`), Detailed Plan §5–§6 |
| **C-M1** | 🟡 Major | Stage 5 (#3), Stage 6 (M1, M3), Stage 7 (M1), Stage 9 (M8) | §4, §5 (`Per-Harness Config Materialization`), Detailed Plan §3 (`writers.go`) |
| **C-M2** | 🟡 Major | Stage 6 (M2), Stage 7 (Structural M2) | §6.2 (`aide gateway list`), §7, NFRs (`Performance`) |
| **C-M3 (TM-2..5)** | 🟡 Major | Stage 7 (TM-M1, TM-M2, TM-M3, TM-M4) | §2, §3, §4, NFRs (`Security & Privacy`) |
| **C-M4** | 🟡 Major | Phase 2 Stage 5 (#1), Stage 6 (M1–M2), Stage 7 (M1, M3), Stage 9 (M4, M6) | §5, §6.3, Detailed Plan §3 (`model.go`, `writers.go`) |
| **C-M5** | 🟡 Major | Phase 2 Stage 5 (#3), Stage 6 (M3), Stage 7 (M2), Stage 8 (M1–M2) | §6.1 (`aide gateway add`), Detailed Plan §5 |
| **C-N1** | 🟢 Nit | Stage 5 (#4, #5), Stage 7 (Structural N3–N7), Stage 9 (N9, N10) | §1 (Mermaid), Detailed Plan §3, §5, §6 (`Verification Plan`) |
