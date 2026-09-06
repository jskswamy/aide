# Skip Secret Decryption in `aide sync` When Nothing Secret-Related Changed

## Problem

`aide sync` unconditionally decrypts the context's age-encrypted secrets
file on every run (`resolveMCPSecretsForSync`, `cmd/aide/sync.go:341-372`)
so it can resolve `{{ .secrets.X }}` placeholders in MCP server `env`
maps before diffing desired config against what's installed in the
target agent. This is necessary for correctness today: `mcpEqual`
(`internal/provision/plan.go:272-288`) does template-blind whole-map
string comparison, so an unresolved `{{ .secrets.X }}` literal would
never match the real installed value and would force a spurious
`OpUpdate` on every single run.

The problem: this couples routine, no-op sync runs to age-key
availability and plaintext-secret handling, even when nothing about the
secrets or the declared MCP config changed since the last successful
sync. Concretely, `aide sync` runs as part of a home-manager
activation, a context where requiring an age identity to be present for
every activation — even ones that touch nothing secret-related — is
both a security-surface concern (plaintext passes through the process
on every run) and an operational one (the key may not be available in
that environment).

Separately, `internal/launcher` (`launcher.go:311-338`) has its own,
independently-implemented decrypt sequence for a different purpose
(injecting secrets as real env vars into the launched agent process).
The two call sites duplicate the same discover-key/decrypt/build-map
logic for unrelated reasons.

## Goals

- Routine `aide sync` runs, where neither `config.yaml` nor the
  encrypted secrets file changed since the last successful sync, never
  discover the age key, decrypt anything, or handle plaintext secrets.
- Sync's correctness is fully retained: any run where a secret was
  rotated, or where the declared MCP config changed in a way that
  needs a resolved secret value, still decrypts and diffs exactly as
  today.
- The duplicated discover-key/decrypt/build-`TemplateData` sequence in
  `sync.go` and `launcher.go` is consolidated into one function.
- A manual escape hatch exists to force full secret resolution and
  re-apply, for the one known-acceptable regression (see Known
  Limitations).

## Non-goals

- Per-server or per-env-key granularity in the diff/apply engine.
  `mcpEqual` and the apply paths (`engine.go`) stay whole-server,
  whole-map — this design gates *whether to decrypt at all*, not how
  the diff itself is computed once secrets are resolved.
- Changing how `internal/launcher` decrypts secrets for agent-process
  env injection at launch time. That call site's *frequency* (once per
  launch) isn't the problem being solved here; only its *duplicated
  implementation* is in scope, via the consolidation goal.
- Detecting or healing secret values that were manually edited/corrupted
  directly in the installed agent's own config file (e.g. hand-editing
  `~/.claude.json`) without `config.yaml` or the secrets file changing.
  See Known Limitations for why this is an accepted trade-off with an
  escape hatch, not a goal.

## Design

### Hash gate

Add `SecretsHash` to `ContextState` (`internal/provision/state.go:39-47`),
alongside the existing `ConfigHash`:

```go
type ContextState struct {
    ConfigHash     string
    HookConfigHash string
    SecretsHash    string // sha256 of the raw encrypted secrets file bytes (ciphertext only, never plaintext)
    SyncedAt       time.Time
    Plugins, MCPServers, Marketplaces map[string]ManagedItem
    Hooks          []ManagedHook
}
```

Computed with the same sha256-over-file-bytes helper `ConfigHash`
already uses (`internal/provision/confighash.go`), just pointed at the
context's resolved secrets path instead of `config.yaml`. A context
with no secrets file hashes to a fixed sentinel (e.g. sha256 of an
empty byte slice) so "no secrets file" produces a stable, comparable
value across runs rather than an empty/zero string that could
coincidentally collide with an unset-state zero value.

Before `resolveMCPSecretsForSync` runs, `runSync` checks: does the
current `config.yaml` hash equal `state.ConfigHash`, AND does the
current secrets-file hash equal `state.SecretsHash`? If both match,
skip secret discovery/decryption/resolution entirely for this run —
proceed to `ComputePlan` using the desired config as-is for any server
that has no `{{ }}` template in its env (servers with unresolved
templates simply won't appear as needing changes, because nothing
about their inputs changed either). If either hash differs, run
exactly today's path: discover key, decrypt, resolve templates, diff,
apply.

This works because the two hashes together capture every source of
legitimate drift for secret-templated values: `SecretsHash` catches
rotation (ciphertext changed), `ConfigHash` catches any declared change
to which servers/keys reference secrets or which non-secret fields
changed (forcing a whole-server rewrite that would need the resolved
secret value anyway, since apply always writes the complete env map —
see Research below). Nothing else can cause a secret-templated value to
need re-resolution.

On first sync for a context (no prior state, or upgrading from a
version without `SecretsHash`), the stored hash is empty/absent and
will never equal a real computed hash — this naturally falls through
to the full path with no special-casing.

Both hashes are only persisted to state together, in the existing
`updateStateAfterSync` (`sync.go:245-321`), and only after a fully
successful sync. They must never be written independently of each
other or after a partial/failed apply, or the gate could incorrectly
skip a run that actually needs resolution.

### `--force-secrets` escape hatch

New bool flag on `aide sync`, alongside `--context`/`--plan`/`--yes`
(`sync.go:42-44`). When set, bypasses the hash gate unconditionally —
same effect as if both hashes had changed. This is the answer to the
one accepted regression: if the installed agent's config was manually
edited/corrupted independent of `config.yaml` or the secrets file,
`--force-secrets` re-derives and re-applies the correct resolved
values on demand.

### Consolidation

New function in `internal/secrets`, e.g.:

```go
func LoadTemplateData(secretsPath string) (config.TemplateData, error)
```

wrapping the existing `DiscoverAgeKey` → `DecryptSecretsFile` →
build-`TemplateData` sequence. Replaces the independently-duplicated
logic in `resolveMCPSecretsForSync` (`sync.go:341-372`) and the
launcher's equivalent block (`launcher.go:311-338`) — both callers get
identical error messages and behavior for free, and there's one place
to fix if the sequence ever needs to change.

## Research Findings

Verified directly against the current implementation before designing
around it (not assumed from the surrounding comments):

- `mcpEqual` (`plan.go:272-288`) does template-blind, whole-map string
  comparison of `Env` (plus `Command`/`URL`/`Args`) — any single
  env-key difference marks the *entire* server as one `OpUpdate`; there
  is no per-key granularity today.
- Both apply paths write the complete env map on every update, never a
  partial patch: the CLI-install path (`applyMCPInstallerOp`,
  `engine.go:228-247`, e.g. Claude's `claude mcp add-json` with the
  full JSON body) and the file-handler path (`engine.go:158-179`,
  `prev[op.Name] = *op.MCP` wholesale replacement before `Write`).
  Confirms that whenever a server's env genuinely needs to change, the
  full resolved map — including unchanged secret values — is required
  regardless of this design; the hash gate only avoids doing that work
  when nothing changed at all.
- `ConfigHash` (`confighash.go:14-24`) already exists and is already
  sha256-over-file-bytes for `config.yaml` — `SecretsHash` reuses the
  identical approach and helper, pointed at a different path.
- `config.IsTemplate` (`internal/config/template.go:49`) is a plain
  `strings.Contains(s, "{{")` check — presence of a secret reference in
  an env value is already detectable without decrypting anything, which
  is what makes hashing (rather than deep template inspection) a
  sufficient and simpler gate.

## Known Limitations

**Manual drift in the installed agent's own config is no longer
self-healed automatically.** Today, because sync always recomputes and
rewrites the resolved env on every run, a hand-edited/corrupted secret
value in e.g. `~/.claude.json` gets silently repaired on the next sync.
With the hash gate, that stops happening when neither `config.yaml` nor
the secrets file changed — sync trusts the installed state in that
case. `--force-secrets` is the explicit remedy; this trade-off was
discussed and accepted in favor of not requiring the age key present
for every routine, unmodified-state sync run.

## Testing

- `resolveMCPSecretsForSync`-level test asserting the decrypt function
  is **not called** (spy/counter, same pattern as `fakeProv` elsewhere
  in this codebase) when `ConfigHash` and `SecretsHash` both match
  state.
- Test that a changed secrets file (different ciphertext, unchanged
  `config.yaml`) forces the full decrypt+resolve+diff+apply path and
  updates `SecretsHash` in state afterward.
- Test that a changed `config.yaml` (e.g. new MCP server referencing a
  secret) forces the full path even when `SecretsHash` is unchanged.
- Test that `--force-secrets` bypasses the gate unconditionally.
- Test the consolidated `secrets.LoadTemplateData` helper directly (key
  discovery failure, decrypt failure, success), then confirm both
  `sync.go` and `launcher.go` delegate to it rather than reimplementing
  the sequence.
- Test that both hashes are only persisted together, and only after a
  fully successful sync (no partial-write scenario leaves the state
  inconsistent).
