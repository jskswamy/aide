# EGM Zoom Out Review (Stages 5 & 8): `docs/designs/gateway/design.md`

**Target Document:** [`docs/designs/gateway/design.md`](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md) (lines 1–766)
**Standard Applied:** `zoom-out-standard.md`

---

## Executive Assessment

1. **Context Sufficiency (§0):** **Pass.** The document defines the problem (fragmented model discovery across Claude Code, Pi, OpenCode, and Codex; `$EDITOR` friction for secret setup; stale model catalogues; plaintext token risks), defines explicit Goals and Non-Goals, and anchors the design in four concrete CUJs.
2. **Root Problem Focus (§1):** **Pass.** Declaring a gateway once under `profiles.<profile>.gateways.<name>` and letting `aide` derive per-harness environment bindings and native model catalogue files solves the root problem and aligns with `aide`'s configuration-plus-convention pattern (`config_dir_name`, `credentials`).
3. **Simplicity, Anti-Weeds & Reuse (§2, §3, §4):** **Pass with Major & Nit findings (0 Blockers, 3 Majors, 2 Nits).** Three areas introduce avoidable state/retry complexity or diverge from existing codebase utilities and conventions.

---

## Findings

### 1. 🟡 Major: Avoid Mutating `Context.Credentials` In-Place with Shadow Tracking Maps on `config.Context`
* **Affected Scope:** Technical Plan §2 ([lines 141, 473](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L141)), Detailed Implementation Plan §1 ([lines 513–558](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L513-L558))
* **Audit Vector:** Simplicity & Extensibility (§2 Minimalist Data Flow) / Reuse Existing (§4 Pattern Consistency)
* **Simplicity Rationale:**
  * The design proposes mutating `ctx.Credentials` and `ctx.DefaultCredentials` in-place via `SynthesizeGatewayCredentials(&ctx)` and adding two unexported shadow-tracking maps (`synthesizedCreds map[string]bool`, `synthesizedDefaultCreds map[string]string`) onto [`config.Context`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/schema.go#L110-L149) so [`Context.MarshalYAML()`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/schema.go#L153-L206) can strip synthesized entries back out when saving `config.yaml`.
  * [`internal/config/credentials.go`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L389-L437) already establishes the pattern for derived in-memory bindings via [`TranslateLegacyCredentials(ctx Context, store string) (Context, []LegacyCredential, error)`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L389): it takes `ctx` by value, clones `ctx.Credentials` and `ctx.DefaultCredentials` on the returned copy, and never mutates the on-disk `cfg.Contexts[name]` map.
* **Recommendation:**
  * Drop `synthesizedCreds` and `synthesizedDefaultCreds` from `config.Context`, and keep `Context.MarshalYAML()` as a straightforward serializer (`if len(c.Gateways) > 0 { out["gateways"] = c.Gateways }`).
  * Make `SynthesizeGatewayCredentials(ctx Context) (Context, error)` a pure value-returning transform (mirroring `TranslateLegacyCredentials`), invoked in `cfg.validateCredentials()` during validation and in `profileCredentials()` in [`internal/launcher/credentials.go`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/launcher/credentials.go#L180-L203) during launch/preview planning.

---

### 2. 🟡 Major: Contradictory HTTP Timeout/Retry Spec & Launch-Blocking Backoff Complexity
* **Affected Scope:** Technical Plan §4 ([line 180](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L180)) vs. Non-Functional Requirements -- Performance & Latency ([lines 498–499](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L498-L499))
* **Audit Vector:** Simplicity & Extensibility (§2 YAGNI / Premature Complexity)
* **Simplicity Rationale:**
  * Line 180 specifies *"using a 20-second timeout and up to 3 attempts with backoff on transient 5xx/network errors"*, whereas line 498 specifies *"a strict `10s` `context.WithTimeout` (`http.Client{Timeout: 10 * time.Second}`)"*.
  * `FetchModels` is called synchronously on the critical path of `Launcher.Launch` when the cache TTL expires (lines 433–443). A 3-attempt retry loop with a 20s timeout per attempt can stall interactive agent startup for up to 60+ seconds when a user is offline or disconnected from a corporate VPN--defeating the fast offline fallback goal in line 499.
* **Recommendation:**
  * Remove the multi-attempt retry/backoff loop (YAGNI) and standardize on a single bounded request with a `10s` timeout (`4 MiB` `io.LimitReader`). On launch-time refresh failure, immediately log the warning and fall back to the cached catalogue.

---

### 3. 🟡 Major: Reuse Existing Codebase Utilities (`fsutil.AtomicWrite`, `statedir.Path`, `launchConfigDir`) and Align `AgeIdentity` Recipient Derivation
* **Affected Scope:** Technical Plan §3–§5 ([lines 163–176, 188, 210–214](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L163-L214)), Non-Functional Requirements ([line 494](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L494)), Detailed Implementation Plan §2–§3 ([lines 576–586, 645–672](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L576-L672))
* **Audit Vector:** Reuse Existing (§4 Codebase Reuse & Pattern Consistency)
* **Simplicity Rationale:**
  1. **Atomic File Writes:** Reuse `fsutil.AtomicWrite(path, data)` used across `internal/secrets/manager.go`, `internal/provision/mcp/opencodejson.go`, and `internal/provision/mcp/codextoml.go`.
  2. **State Directory Signature:** `statedir.Path(homeDir, name string) string` requires an explicit `homeDir` so tests remain hermetic against the host's `$HOME`.
  3. **Config Directory Resolution Duplication:** `gateway.HarnessConfigDir` duplicates `launchConfigDir` in `internal/launcher/native_auth.go`.
  4. **`AgeIdentity` Recipient Derivation:** `DiscoverAgeKey()` returns `*AgeIdentity{Source, KeyData}` (not a `filippo.io/age.Identity`), where `KeyData` may be a file path (`keys.txt`) or empty (`SourceYubiKey`).
* **Recommendation:**
  * Reuse `fsutil.AtomicWrite` in `internal/gateway/cache.go` and `internal/gateway/writers.go`.
  * Pass `homeDir string` into `CachePath`, `ReadCache`, `WriteCache`, and `RemoveCache`.
  * Share `launchConfigDir` logic across `launcher` and `gateway`.
  * Add `(id *AgeIdentity) Recipient() (string, error)` in `internal/secrets/age.go`.

---

### 4. 🟢 Nit: Align Struct & Receiver Names with Existing Codebase Symbols
* **Affected Scope:** Non-Functional Requirements ([line 493](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L493)), Detailed Implementation Plan §1 & §4 ([lines 557, 707–709](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L707-L709))
* **Recommendation:** Update lines 493, 557, and 707–709 to use `(c *Config) CredentialScrubNames()` and `rc *aidectx.ResolvedContext` / `rc.Name`.

---

### 5. 🟢 Nit: Clarify `opencode.jsonc` Comment Handling to Match `internal/provision/mcp/opencodejson.go`
* **Affected Scope:** Detailed Implementation Plan §3 ([line 698](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L698))
* **Recommendation:** Reword line 698 to *"with JSONC comments accepted on read via `hujson.Standardize` and sibling keys preserved on write"*, matching `internal/provision/mcp/opencodejson.go`.
