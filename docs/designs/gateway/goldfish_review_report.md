# Goldfish Annotated Review Report

Status: resolved
**Target:** [`docs/designs/gateway/design.md`](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md) (`1..831`)

## Resolution Summary
- **Accepted & Applied (`PROPOSE FIX`)**: 9 consolidated items (`C-B1` through `C-B5`, `C-M1` through `C-M3` including Threat Modeling mitigations `TM-1` through `TM-5`, and `C-N1`).
- **Contested (`CONTEST`)**: 0
- **Escalated (`ESCALATE`)**: 0
- **Declined (`DECLINE`)**: 0

## Applied Fixes

1. **`C-B1` (`internal/config/` & `internal/launcher/credentials.go`)**:
   - Replaced non-existent `ValidateCredentials` and `ResolveCredentials` references with `(*Config).validateCredentials()` and `profileCredentials()`.
   - Updated `SynthesizeGatewayCredentials(ctx Context) (Context, error)` to return a cloned `Context` by value (following `TranslateLegacyCredentials`), setting `Backend: CredentialBackendAge` on synthesized bindings, iterating `slices.Sorted(maps.Keys(ctx.Gateways))`, rejecting collisions with explicit `ctx.Credentials` entries, and only auto-appending to `DefaultCredentials` when routes do not collide in `checkDefaultRoutes`.
   - Fixed receiver on `(*Config).CredentialScrubNames()` and type of `projectDefiningKeys` (`[]string`).
2. **`C-B2` & `TM-1` (`internal/gateway/fetch.go`, `internal/config/gateway.go`)**:
   - Standardized `/v1/models` HTTP contract across §4, NFRs, and Detailed Plan §3 (`GET <NormalizeBaseURL(baseURL)>/v1/models?limit=1000`, `10s` timeout / `5s` background refresh timeout, `4 MiB` `io.LimitReader`).
   - Required `https://` for non-loopback `base_url` hosts, blocked cross-origin and `https` $\rightarrow$ `http` redirects via `http.Client.CheckRedirect`, and rejected embedded `user:pass@`, `?query`, and `#fragment` in `base_url`.
3. **`C-B3` (`internal/launcher/launcher.go`)**:
   - Updated `ensureGatewaySynced` signature to use `rc *aidectx.ResolvedContext` (`rc.Name`), `credPlan CredentialPlan`, and `homeDir string`, running after `ResolveCredentialEnv` and skipping under `--auth login` or read-only `aide which` / `aide status`.
4. **`C-B4` & `TM-4` (`internal/gateway/cache.go`)**:
   - Updated `CachePath`, `ReadCache`, `WriteCache`, and `RemoveCache` to take `homeDir string` and call `statedir.Path(homeDir, filepath.Join("gateways", profile, gatewayName+".json"))` with path-traversal validation.
5. **`C-B5` & `TM-5` (`internal/secrets/age.go`, `internal/secrets/manager.go`)**:
   - Pointed `DecryptSecretsFile` link to `internal/secrets/sops.go`, added `(id *AgeIdentity) Recipient() (string, error)`, specified in-memory encryption without `/tmp` plaintext files in `SetKey`/`DeleteKey`, specified `os.Remove(srcPath)` when `DeleteKey` deletes the last remaining key in a store, and specified cross-profile reference checking before deleting a secret key on `aide gateway remove`.
6. **`C-M1` & `TM-2`, `TM-3` (`internal/gateway/writers.go`, `internal/gateway/fetch.go`)**:
   - Defined `InspectHarness` predicates (`StatePending`, `StateStale`, `StateSynced`) for all four harnesses, Claude multi-gateway default selection, control-character sanitization on `/v1/models` strings, and `gw.Key` regex + loader/proxy/`NeverAllowEnv` blocklist validation.
7. **`C-M2` & `C-N1` (`cmd/aide/gateway.go`)**:
   - Added `GatewayListEntry` JSON schema for `aide gateway list --format json`, unified launch-time fallback warning messages, aligned CLI constructor names (`gatewayCmd()`, etc.), and updated Manual Verification to use `aide which`.
