# Seatbelt Guard Architecture Inversion

**Date:** 2026-03-24
**Status:** Draft
**Scope:** `pkg/seatbelt/`, `internal/ui/banner.go`

## Problem

The seatbelt guard system is built on the false premise that macOS seatbelt uses
"last-rule-wins" semantics. Empirical testing (2026-03-24) proves that **deny
always wins over allow**, regardless of rule order. This breaks any guard that
uses the deny-broad/allow-narrow pattern.

The SSH keys guard is immediately broken: it denies all of `~/.ssh` via subpath,
then tries to grant back `known_hosts` and `config` — those grants are silently
ignored by seatbelt.

Other credential guards work by accident (they only deny, no exceptions needed)
but are built on the wrong abstraction.

Additionally, guards emit rules for paths that may not exist on the user's
system, producing unnecessary noise and no useful protection.

## Solution

1. Invert the guard architecture from deny-broad/allow-narrow to
   allow-broad/deny-narrow
2. Collapse the three-tier RuleIntent system (Setup/Restrict/Grant) to two tiers
   (Allow/Deny) that match actual seatbelt semantics
3. Introduce `GuardResult` for rich guard diagnostics (protected paths, allowed
   exceptions, skipped paths, env var overrides)
4. Add existence checks to all credential guards — skip with warning when target
   paths don't exist
5. Add discovery (ReadDir + allowlist) to the SSH keys guard
6. Change kubernetes guard from default to opt-in
7. Redesign the startup banner for transparency, grouped by guard status

## Design

### Core Domain Model

#### RuleIntent (module.go)

Collapse from three tiers to two:

```go
const (
    Allow RuleIntent = 100 // broad infrastructure + directory allows
    Deny  RuleIntent = 200 // narrow specific-file/path denials
)
```

`Setup`, `Restrict`, `Grant` are removed. Sort order is cosmetic — allows
rendered first, then denies. Deny wins regardless of position. The sort is
kept for readability: generated profiles read top-to-bottom as "here's what's
allowed, here's what's denied within that," making them auditable.

Comments updated to reflect actual seatbelt behavior:

```go
// RuleIntent determines a rule's position in the rendered profile.
// The renderer stable-sorts rules by intent: allows first, then denies.
// Seatbelt uses deny-wins-over-allow semantics — deny rules always
// take precedence regardless of position. The sort order is for
// readability only.
```

#### Rule Constructors

| Old                  | New               |
|----------------------|-------------------|
| `SetupRule(text)`    | `AllowRule(text)`  |
| `RestrictRule(text)` | `DenyRule(text)`   |
| `GrantRule(text)`    | `AllowRule(text)`  |
| `SectionSetup(n)`    | `SectionAllow(n)`  |
| `SectionRestrict(n)` | `SectionDeny(n)`   |
| `SectionGrant(n)`    | `SectionAllow(n)`  |

`Allow()`, `Deny()`, `Raw()`, `Comment()`, `Section()` stay as convenience
constructors with updated intents.

#### GuardResult

Replaces `[]Rule` as the return type from `Module.Rules()`:

```go
type GuardResult struct {
    Name      string     // guard name, set by the profile builder from Module.Name()
    Rules     []Rule
    Protected []string   // paths being denied
    Allowed   []string   // paths explicitly allowed (exceptions)
    Skipped   []string   // "~/.config/op not found" etc.
    Overrides []Override // env var overrides detected
}

type Override struct {
    EnvVar      string // "KUBECONFIG"
    Value       string // "/custom/kubeconfig"
    DefaultPath string // "~/.kube/config"
}
```

#### Module Interface

```go
type Module interface {
    Name() string
    Rules(ctx *Context) GuardResult
}
```

The `Guard` interface already has `Type()` and `Description()` methods on top
of `Module`. Its signature is unchanged — only the `Module` interface it embeds
gets the new `GuardResult` return type.

### Guard Changes

#### Universal Existence-Check Pattern

Every credential guard checks if its target paths exist before emitting deny
rules. Missing paths are reported in `GuardResult.Skipped`. Guards that protect
multiple independent paths (like password-managers) check each one individually.

```go
func (g *someGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    result := seatbelt.GuardResult{}
    path := ctx.HomePath(".some-tool")

    if !dirExists(path) {
        result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", path))
        return result
    }

    // emit rules, populate result.Protected
    return result
}
```

No hard failures. Missing directory = no deny rules + skip message. I/O errors
= same treatment (skip with warning, sandbox always starts).

#### SSH Keys Guard — Full Rewrite

The SSH keys guard is the only guard that needs structural redesign. Algorithm:

1. Check `~/.ssh/` exists — if not, skip with warning
2. `ReadDir` to list all entries
3. For each regular file entry, check against safe-file allowlist:
   - Exact names: `known_hosts`, `known_hosts.old` (backup from `ssh-keygen -R`),
     `config`, `authorized_keys`, `environment`
   - Suffix pattern: `*.pub` (matched via `strings.HasSuffix(name, ".pub")`)
4. Matching entries — emit `AllowRule` (literal), add to `result.Allowed`
5. Everything else — emit `DenyRule` (literal), add to `result.Protected`
6. Skip subdirectories (like `sockets/`, `config.d/`) — deny only regular files
   and symlinks to files (a symlink like `id_rsa -> ../actual_key` is treated
   as a potential key and denied unless it matches the allowlist)

The intent is to deny all private key material by default, including files with
user-chosen names (e.g., `my-deploy-key`, `prod-bastion`). The allowlist is
intentionally narrow: only files known to be safe are allowed through;
everything else is assumed to be sensitive.

No content scanning. No reading file bytes. Just filename matching against a
known-safe allowlist, then deny everything else.

#### Kubernetes Guard — Change to Opt-in

Change `Type()` return from `"default"` to `"opt-in"`. Blocking kubeconfig
prevents all kubectl interaction, which defeats the purpose for k8s-heavy
agent workflows. Users who want the protection can explicitly enable it.

#### All Other Credential Guards — Mechanical Updates

Each guard gets:
- Existence check per target path/directory
- Return `GuardResult` instead of `[]Rule`
- Populate `Protected` with actual denied paths
- Populate `Skipped` for missing paths
- Populate `Overrides` when env var overrides are detected
- Replace `RestrictRule` with `DenyRule`, `GrantRule` with `AllowRule`

Use `fmt.Sprintf` for all string formatting — no `+` concatenation.

#### Helpers (helpers.go)

Update return types from `RestrictRule`/`GrantRule` to `DenyRule`/`AllowRule`:

```go
func DenyDir(path string) []seatbelt.Rule {
    return []seatbelt.Rule{
        seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
        seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
    }
}

func DenyFile(path string) []seatbelt.Rule {
    return []seatbelt.Rule{
        seatbelt.DenyRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
        seatbelt.DenyRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
    }
}

func AllowReadFile(path string) seatbelt.Rule {
    return seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
}
```

### Profile Renderer Changes

**`profile.go` (sort + aggregation):**

The `Render()` method in `profile.go` currently collects rules from all modules,
tags each with its source module name, stable-sorts by intent, and calls
`renderTaggedRules`. Changes:

- Sort by two intents instead of three (Allow before Deny)
- After collecting rules, also aggregate `GuardResult` metadata (Protected,
  Allowed, Skipped, Overrides) from each module into a `ProfileResult` that the
  caller can pass to the banner layer

```go
type ProfileResult struct {
    Profile string         // rendered seatbelt profile text
    Guards  []GuardResult  // per-guard diagnostics for banner display
}
```

`Render()` returns `ProfileResult` instead of just the profile string. The
caller (launcher) maps `GuardResult` data into `GuardDisplay` structs for the
banner.

**`render.go` (text rendering):**

`render.go` contains `renderRules` and `renderTaggedRules` — low-level text
formatting. These functions operate on `[]Rule` and `[]taggedRule` respectively.
No structural changes needed — they already work with the `Rule` type's
`String()` method. The only change is that rules now carry `Allow`/`Deny`
intents instead of `Setup`/`Restrict`/`Grant`, which is transparent to the
renderer.

### Banner Redesign

#### Data Model

Replace `SandboxInfo` with richer guard-aware data:

```go
// Replaces the current SandboxInfo in internal/ui/banner.go which has:
// Disabled, Network, Ports, GuardCount, Denied, Guards, Protecting fields.
type SandboxInfo struct {
    Disabled  bool
    Network   string   // "outbound only", "unrestricted", "none"
    Ports     string   // "all" or "443, 53"
    Active    []GuardDisplay
    Skipped   []GuardDisplay
    Available []string // opt-in guard names not enabled (no per-path data)
}

// GuardDisplay is a UI-layer type in internal/ui. Override is imported from
// pkg/seatbelt since it is shared between the profile builder and the banner.
type GuardDisplay struct {
    Name      string
    Protected []string
    Allowed   []string
    Overrides []seatbelt.Override
    Reason    string   // for skipped: "~/.kube not found"
}
```

#### Rendering

Three groups, visually separated:

- **Active** (green `✓`) — guards with rules, show denied/allowed/overrides
- **Skipped** (yellow `⊘`) — guards that checked but found nothing, one-liner
  with reason
- **Available** (dim `○`) — opt-in guards not enabled, collapsed to one line

Long lists capped at 3 items with `(+N more)` suffix.

When any list is truncated or guards are skipped/available, show a dimmed hint:
`run 'aide sandbox' for full details`

Network displayed as `network: outbound only` (not raw "outbound").

#### Example Output (Compact Style)

```
🔧 aide · my-project (claude)
   📁 matched: ~/source/my-project
   🛡 Sandbox
         network: outbound only

     ✓ ssh-keys
         denied:  ~/.ssh/id_rsa, ~/.ssh/id_ed25519
         allowed: ~/.ssh/known_hosts, ~/.ssh/config
     ✓ cloud-aws
         denied:  ~/.aws/credentials, ~/.aws/config
         override: AWS_CONFIG_FILE → /custom/aws (default: ~/.aws/config)
     ✓ password-managers
         denied:  ~/.config/op, ~/.password-store, ~/.gnupg/private-keys-v1.d (+3 more)

     ⊘ kubernetes — ~/.kube not found
     ⊘ browsers — Chrome profile not found

     ○ docker, github-cli, npm, netrc, vercel — available (opt-in)

     run `aide sandbox` for full details
```

#### Example Output (Boxed Style)

```
┌─ aide ───────────────────────────────────────────
│ 🎯 Context   my-project
│ 📁 Matched   ~/source/my-project
│ 🤖 Agent     claude
│
│ 🛡 Sandbox
│        network: outbound only
│
│    ✓ ssh-keys
│        denied:  ~/.ssh/id_rsa, ~/.ssh/id_ed25519
│        allowed: ~/.ssh/known_hosts, ~/.ssh/config
│    ✓ cloud-aws
│        denied:  ~/.aws/credentials, ~/.aws/config
│        override: AWS_CONFIG_FILE → /custom/aws (default: ~/.aws/config)
│    ✓ password-managers
│        denied:  ~/.config/op, ~/.password-store, ~/.gnupg/private-keys-v1.d (+3 more)
│
│    ⊘ kubernetes — ~/.kube not found
│    ⊘ browsers — Chrome profile not found
│
│    ○ docker, github-cli, npm, netrc, vercel — available (opt-in)
│
│    run `aide sandbox` for full details
└──────────────────────────────────────────────────
```

#### Example Output (Clean Style)

```
aide · context: my-project
  Agent     claude
  Matched   ~/source/my-project

  Sandbox
       network: outbound only

    ✓ ssh-keys
        denied:  ~/.ssh/id_rsa, ~/.ssh/id_ed25519
        allowed: ~/.ssh/known_hosts, ~/.ssh/config
    ✓ cloud-aws
        denied:  ~/.aws/credentials, ~/.aws/config
        override: AWS_CONFIG_FILE → /custom/aws (default: ~/.aws/config)
    ✓ password-managers
        denied:  ~/.config/op, ~/.password-store, ~/.gnupg/private-keys-v1.d (+3 more)

    ⊘ kubernetes — ~/.kube not found
    ⊘ browsers — Chrome profile not found

    ○ docker, github-cli, npm, netrc, vercel — available (opt-in)

    run `aide sandbox` for full details
```

### Scope Boundary

#### Changed

- `pkg/seatbelt/module.go` — RuleIntent, GuardResult, Override, Module interface,
  rule constructors
- `pkg/seatbelt/profile.go` — Renderer sort logic, GuardResult collection
- `pkg/seatbelt/render.go` — No structural changes; intent values are
  transparent to the text renderer
- `pkg/seatbelt/guards/guard_ssh_keys.go` — Full rewrite with discovery
- `pkg/seatbelt/guards/guard_kubernetes.go` — Change to opt-in
- `pkg/seatbelt/guards/guard_password_managers.go` — Per-tool existence checks
- `pkg/seatbelt/guards/guard_cloud_aws.go` — Existence check, overrides
- `pkg/seatbelt/guards/guard_cloud_gcp.go` — Existence check, overrides
- `pkg/seatbelt/guards/guard_cloud_azure.go` — Existence check, overrides
- `pkg/seatbelt/guards/guard_cloud_digitalocean.go` — Existence check
- `pkg/seatbelt/guards/guard_cloud_oci.go` — Existence check, overrides
- `pkg/seatbelt/guards/guard_terraform.go` — Existence check, overrides
- `pkg/seatbelt/guards/guard_vault.go` — Existence check
- `pkg/seatbelt/guards/guard_browsers.go` — Existence check
- `pkg/seatbelt/guards/guard_docker.go` — Existence check
- `pkg/seatbelt/guards/guard_github_cli.go` — Existence check
- `pkg/seatbelt/guards/guard_npm.go` — Existence check
- `pkg/seatbelt/guards/guard_netrc.go` — Existence check
- `pkg/seatbelt/guards/guard_vercel.go` — Existence check
- `pkg/seatbelt/guards/guard_aide_secrets.go` — Existence check
- `pkg/seatbelt/guards/helpers.go` — Update rule constructors
- `internal/ui/banner.go` — New SandboxInfo, GuardDisplay, grouped rendering
- All existing tests updated to match new interfaces

#### Not Changed

- Always-guards (base, system-runtime, network, filesystem, keychain,
  node-toolchain, nix-toolchain, git-integration) — infrastructure guards,
  no credential paths to existence-check
- Agent modules (modules/) — config directory access, not credentials
- Custom guard — follows new interface but logic unchanged

### Testing Strategy

- **Guard unit tests:** Mock `Context` with existing and missing paths, verify
  `GuardResult` fields (rules emitted, protected/skipped/allowed populated
  correctly). Each guard tested with all-paths-exist and no-paths-exist cases.
- **SSH keys guard:** Create temp `~/.ssh/` with mix of key files and safe
  files, verify allowlist matching (`known_hosts`, `*.pub`, etc.) and
  deny-everything-else behavior. Test with empty directory, only pub files,
  only private keys, and mixed.
- **Profile integration:** Render a full profile via `Render()`, verify deny
  rules appear after allow rules, verify `ProfileResult` contains aggregated
  `GuardResult` data from all modules, verify no three-tier intent values remain.
- **GuardResult-to-banner flow:** Test that `ProfileResult.Guards` maps
  correctly into `SandboxInfo` with proper active/skipped/available grouping.
  This is the seam between the renderer and the UI layer.
- **Banner tests:** Verify all three styles (compact, boxed, clean) render
  correctly with active/skipped/available guards, verify list truncation at 3
  items with `(+N more)` suffix, verify hint line appears when truncated.
- **Regression:** Verify no guard uses the old deny-broad/allow-narrow pattern
  (grep for `RestrictRule`, `GrantRule`, `SectionRestrict`, `SectionGrant`).
