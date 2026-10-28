# EGM Comprehension Review (Stage 6): `docs/designs/gateway/design.md`

**Audited Scopes:** `FeatureRange: 1..507`, `DetailedRange: 508..765`
**Standard Applied:** `re-implementation-standard.md` (Zero-Assumption Rule)

---

## Findings

### 🔴 Blocker 1: Direct Contradiction in `/v1/models` HTTP Timeout, URL Path, and Retry Contract
* **Affected Scope:** Section 4 ([lines 179–180](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L179-L180)), Non-Functional Requirements ([line 498](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L498)), and Detailed Plan Section 3 ([lines 637–643](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L637-L643)).
* **Discrepancy & Rationale:**
  * Line 180 specifies `GET {strings.TrimRight(baseURL, "/")}/v1/models?limit=1000` with a 20-second timeout and up to 3 attempts with backoff.
  * Line 498 specifies `<base_url>/models` with a strict `10s` `context.WithTimeout` and `4 MiB` `io.LimitReader`.
  * Lines 637–642 specify `GET <NormalizeBaseURL(baseURL)>/v1/models?limit=1000`.
* **Recommendation:** Reconcile lines 180, 498, and 637–643 around a single HTTP contract (`GET <NormalizeBaseURL(baseURL)>/v1/models?limit=1000`, `10s` timeout, `4 MiB` response body limit).

---

### 🔴 Blocker 2: Unspecified `SynthesizeGatewayCredentials` Rules for `default_credentials`, Multi-Gateway Precedence, and Collisions
* **Affected Scope:** Section 2 ([lines 106–141](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L106-L141)), Section 6.2 ([line 350](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L350)), and Detailed Plan Section 1 ([lines 524, 546–557](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L546-L557)).
* **Discrepancy & Rationale:**
  * `Context.Gateways` is a `map[string]GatewayConfig` with non-deterministic iteration order in Go. The document does not specify how `SynthesizeGatewayCredentials` selects the default credential per harness when multiple gateways support that harness, or whether an explicit `credentials.<name>` / `default_credentials` entry takes precedence or errors on collision.
* **Recommendation:** Specify deterministic ordering (`slices.Sorted(maps.Keys(ctx.Gateways))`), collision rules between `gateways.<name>` and `credentials.<name>`, and when a gateway is auto-added to `DefaultCredentials`.

---

### 🔴 Blocker 3: Launch-Time Ordering Relative to Lazy Profile Provisioning
* **Affected Scope:** Section 7 ([lines 430–449](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L430-L449)), Alternative C ([lines 480–482](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L480-L482)), and Detailed Plan Section 4 ([lines 707–710](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L707-L710)).
* **Discrepancy & Rationale:**
  * Creating `~/.<harness>-<profile>` via `MaterializeHarness(..., createDirIfMissing: true)` before first-launch profile provisioning check runs in `Launcher.Launch` would cause `Launcher.Launch` to think the profile directory was already provisioned.
* **Recommendation:** Specify exact placement in `Launcher.Launch` relative to profile directory provisioning and specify cold-launch behavior when the initial network sync fails and no cache exists on disk.

---

### 🟡 Major 1: Undefined Criteria for `synced`, `stale`, and `pending` in `InspectHarness` and Single-File Contention in Claude
* **Affected Scope:** Section 5 ([lines 208–270](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L208-L270)), Section 6.2 ([lines 356–361](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L356-L361)), Detailed Plan Section 3 ([lines 617–630, 663–669](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L617-L630)).
* **Recommendation:** Define exact conditions for `StatePending`, `StateStale`, and `StateSynced` across `claude`, `pi`, `opencode`, and `codex`, and specify which gateway writes `<claude-dir>/cache/gateway-models.json` when multiple gateways target `claude`.

---

### 🟡 Major 2: Missing Schema for `aide gateway list --format json`
* **Affected Scope:** Goals ([line 31](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L31)), CUJ 2 ([line 55](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L55)), Section 6.2 ([lines 344–372](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L344-L372)).
* **Recommendation:** Define the JSON output schema / Go struct serialized by `aide gateway list --format json`.

---

### 🟡 Major 3: Ambiguities in Model Normalization, Fallback Formatting, and URL Suffix Handling Across Writers
* **Affected Scope:** Section 4 ([lines 181–185](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L181-L185)), Section 5 ([lines 216–268](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L216-L268)), Section 6.1 ([lines 298–299](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L298-L299)).
* **Recommendation:** Specify fallback formatting when `Provider` or `ContextWindow` is zero/empty, how `NormalizeBaseURL` is stored and suffixed per harness (`claude`/`pi` vs `opencode`/`codex`), hostname slug derivation for `<name>`, and model sort key (`Model.ID` ascending).
