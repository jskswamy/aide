# EGM Implementation Readiness & Codebase Details Report (Stage 9): `docs/designs/gateway/design.md`

**Target Scope:** [`docs/designs/gateway/design.md`](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L508-L765) (`DetailedRange: 508..765`, cross-checked against `FeatureRange: 1..507`)
**Standard Applied:** `implementation-readiness-standard.md`

---

## Findings

### 🔴 Blocker 1: Non-Existent Functions & Wrong Receiver Type in `internal/config/credentials.go`
* **Affected Scope:** [`design.md` lines 468, 493, 527, 553–558](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L553-L558) vs. [`internal/config/credentials.go`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L164-L357).
* **Discrepancy:**
  1. `ValidateCredentials(cfg *Config)` does not exist; the actual method is `func (c *Config) validateCredentials() error`.
  2. `ResolveCredentials(ctx Context, flagValue string, agent string)` does not exist; route lookup is `(b CredentialBinding) Route(name, agent string)` and launch planning is `profileCredentials` / `PlanProfileCredentials` in `internal/launcher/credentials.go`.
  3. `(c Context) CredentialScrubNames() []string` has receiver `(c *Config)` in `internal/config/credentials.go:344`.
* **Fix:** Update all references to `(*Config).validateCredentials()`, `profileCredentials()` / `PlanProfileCredentials()`, and `(*Config).CredentialScrubNames()`.

### 🔴 Blocker 2: Missing `Backend: CredentialBackendAge` on Synthesized `CredentialBinding`
* **Affected Scope:** [`design.md` lines 546–550](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L546-L550) vs. [`internal/config/credentials.go:219`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/credentials.go#L219).
* **Discrepancy:** `validateBinding` rejects any non-keyless `CredentialBinding` unless `b.Backend == CredentialBackendAge` (`"age"`).
* **Fix:** Specify setting `Backend: CredentialBackendAge` on every gateway-synthesized `CredentialBinding`.

### 🔴 Blocker 3: Non-Existent Types, Fields, and Functions in `internal/launcher/launcher.go`
* **Affected Scope:** [`design.md` lines 704–711](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L704-L711) vs. [`internal/launcher/launcher.go:260–305`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/launcher/launcher.go#L260-L305).
* **Discrepancy:**
  1. `*config.ResolvedConfig` does not exist; `Launcher.Launch` uses `rc *aidectx.ResolvedContext`.
  2. `rc.ProfileName` does not exist; the field is `rc.Name`.
  3. `ensureProfileDirProvisioned` does not exist in `internal/launcher/launcher.go`.
* **Fix:** Update signature to `func (l *Launcher) ensureGatewaySynced(rc *aidectx.ResolvedContext, harnessName, homeDir string, credPlan CredentialPlan, credEnv map[string]string) error`, called right after `ResolveCredentialEnv`.

### 🔴 Blocker 4: `statedir.Path` Signature Mismatch and Missing `homeDir` Parameter in `internal/gateway/cache.go`
* **Affected Scope:** [`design.md` lines 188, 645–654](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L645-L654) vs. [`internal/statedir/statedir.go:22`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/statedir/statedir.go#L22).
* **Discrepancy:** `statedir.Path(homeDir, name string) string` takes two `string` arguments (`homeDir` and `name`).
* **Fix:** Pass `homeDir string` to `CachePath`, `ReadCache`, `WriteCache`, and `RemoveCache`, calling `statedir.Path(homeDir, filepath.Join("gateways", profile, gatewayName+".json"))`.

### 🔴 Blocker 5: Non-Existent `internal/secrets/decrypt.go`, Missing `AgeIdentity.Recipient()`, and Wrong Default Age Key Path
* **Affected Scope:** [`design.md` lines 163–176, 307, 576–586](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L163-L176) vs. [`internal/secrets/sops.go:22`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/secrets/sops.go#L22) and [`internal/secrets/age.go:25–108`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/secrets/age.go#L25-L108).
* **Discrepancy:**
  1. `DecryptSecretsFile` is in `internal/secrets/sops.go`, not `internal/secrets/decrypt.go`.
  2. `AgeIdentity` is a struct `{Source AgeKeySource; KeyData string}` without a `.Recipient()` method.
  3. `DiscoverAgeKey()` checks `sops/age/keys.txt`, `SOPS_AGE_KEY`, and `SOPS_AGE_KEY_FILE`, not `~/.config/aide/age.key`.
* **Fix:** Reference `internal/secrets/sops.go`, add `func (id *AgeIdentity) Recipient() (string, error)` to `internal/secrets/age.go`, and update line 307 to reference `secrets.DiscoverAgeKey()`.

### 🔴 Blocker 6: Type Mismatch on `projectDefiningKeys` in `internal/config/profiles.go`
* **Affected Scope:** [`design.md` lines 559–563](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L559-L563) vs. [`internal/config/profiles.go:47–51`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/config/profiles.go#L47-L51).
* **Discrepancy:** `projectDefiningKeys` is a `[]string` slice, not a `map[string]bool`.
* **Fix:** Specify appending `"gateways"` to the `projectDefiningKeys` `[]string` slice.

### 🟡 Major 1: Unisolated Profile Config Directory Resolution in `HarnessConfigDir` vs. `launchConfigDir`
* **Affected Scope:** [`design.md` lines 210–213, 660–661](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L210-L213) vs. [`internal/launcher/native_auth.go:66–78`](file:///Users/subramk/source/github.com/jskswamy/aide/internal/launcher/native_auth.go#L66-L78).
* **Fix:** When `!ctx.SelectsConfigDir()`, check `ctx.Env[d.ProfileEnvKey]` and `os.Getenv(d.ProfileEnvKey)` before falling back to `d.DefaultDir(home)`, matching `launchConfigDir`.

### 🟢 Nit 1: CLI Constructor Naming & `--format` Flag Registration in `cmd/aide/`
* **Affected Scope:** [`design.md` lines 344, 722–731](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L722-L731).
* **Fix:** Use `gatewayCmd()`, `gatewayAddCmd()`, `gatewayListCmd()`, `gatewayRemoveCmd()`, `gatewaySyncCmd()`, and reuse the root persistent `--format` flag via `output.FromCmd(cmd)` and `output.Emit`.

### 🟢 Nit 2: Non-Existent `--dry-run` Flag in Manual Verification Step 3
* **Affected Scope:** [`design.md` line 763](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md#L763).
* **Fix:** Replace `aide --profile work --agent pi --dry-run` with `aide which --profile work --agent pi` (for dry-run credential resolution) and `aide --profile work --agent pi` (for launch-time TTL refresh).
