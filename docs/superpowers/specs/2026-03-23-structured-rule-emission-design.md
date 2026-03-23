# Structured Rule Emission

**Date:** 2026-03-23
**Status:** Approved

## Problem

Seatbelt uses **last-rule-wins** semantics. The guard system relied on a false assumption that "literal beats subpath" (specificity-based resolution). In reality, the last matching rule in the profile determines the outcome regardless of path or operation specificity.

This caused:
- SSH `known_hosts` blocked despite literal allow rule (deny appeared after allow in some configurations)
- GPG `pubring.kbx` blocked because `password-managers` guard denied all of `~/.gnupg` instead of just private keys
- Any future guard that emits a deny after another guard's allow would silently break the allow

The root cause: guards emit flat `[]Rule` lists and the rendering order depends on guard registration order. Nothing enforces that allows come after denies. Each guard author must understand seatbelt's last-rule-wins semantics and get the ordering right manually.

## Design

### Rule Intent

Each rule carries an explicit intent that determines its position in the rendered profile:

```go
type RuleIntent int

const (
    Setup    RuleIntent = 100  // infrastructure allows + refinement denies
    Restrict RuleIntent = 200  // block sensitive paths
    Grant    RuleIntent = 300  // re-allow within restricted paths
)
```

**Setup (100):** Rules that establish what the agent CAN do. Broad filesystem allows, network access, system binary paths, mach services, device nodes. Also includes refinement denies that narrow a broad allow (e.g., deny launchd sockets within allowed `/tmp`). These come first in the profile.

**Restrict (200):** Rules that block access to sensitive resources. SSH keys, cloud credentials, browser data, password stores. These come after Setup, so they override infrastructure allows for the specific paths they target.

**Grant (300):** Rules that re-allow specific paths within restricted areas. SSH `known_hosts` within denied `~/.ssh`, custom guard `allowed` entries within denied directories. These come last in the profile, so they always win over Restrict rules for the paths they target.

### Why 3 intents, not 5

Early analysis identified 5 potential intents (Foundation, Enable, Refine, Restrict, Except). In practice:
- Foundation (2 rules) and Enable (~200 rules) and Refine (1 rule) all come from infrastructure guards. Guard registration order handles their internal ordering. Collapsing them into Setup loses nothing because `base` is always registered first (emits `(deny default)` before any allows).
- Restrict and Except map directly to Restrict and Grant.

3 intents cover all current and foreseeable patterns without over-engineering.

### Why gapped numeric values

`Setup=100`, `Restrict=200`, `Grant=300` leave gaps for future intents without renumbering. Guard developers writing Go code can use intermediate values (e.g., `RuleIntent(150)`) for precise positioning. Config users (YAML custom guards) are locked to the three predefined intents.

### Rule Constructors

```go
// SetupRule creates a rule with Setup intent.
func SetupRule(text string) Rule {
    return Rule{intent: Setup, lines: text}
}

// RestrictRule creates a rule with Restrict intent.
func RestrictRule(text string) Rule {
    return Rule{intent: Restrict, lines: text}
}

// GrantRule creates a rule with Grant intent.
func GrantRule(text string) Rule {
    return Rule{intent: Grant, lines: text}
}
```

The existing `Raw()`, `Allow()`, `Deny()` constructors default to `Setup` intent for backward compatibility with agent modules.

`Section()` and `Comment()` rules inherit the intent of the rule that follows them. In practice, section comments are cosmetic and do not affect rendering order.

### Module Interface

Unchanged. Guards and modules return `[]Rule`. Each rule carries its own intent.

```go
type Module interface {
    Name() string
    Rules(ctx *Context) []Rule
}

type Guard interface {
    Module
    Type() string
    Description() string
}
```

### Profile Renderer

The renderer collects all rules from all modules/guards, stable-sorts by intent, and emits:

```go
func (p *Profile) Render() (string, error) {
    var allRules []taggedRule
    for _, m := range p.modules {
        rules := m.Rules(p.ctx)
        for _, r := range rules {
            allRules = append(allRules, taggedRule{module: m.Name(), rule: r})
        }
    }

    // Stable sort by intent — preserves module order within same intent
    sort.SliceStable(allRules, func(i, j int) bool {
        return allRules[i].rule.intent < allRules[j].rule.intent
    })

    return renderTaggedRules(allRules), nil
}
```

The stable sort guarantees:
1. All Setup rules come first (in guard registration order)
2. All Restrict rules come second (in guard registration order)
3. All Grant rules come last (in guard registration order)
4. Within the same intent, guard registration order is preserved

### Section Comments

Section comments need explicit intent so they sort alongside their rules. Intent-specific section constructors:

```go
func SectionSetup(name string) Rule    { return Rule{intent: Setup, comment: "--- " + name + " ---"} }
func SectionRestrict(name string) Rule { return Rule{intent: Restrict, comment: "--- " + name + " ---"} }
func SectionGrant(name string) Rule    { return Rule{intent: Grant, comment: "--- " + name + " ---"} }
```

Guards use the matching section constructor:

```go
// ssh-keys guard
return []Rule{
    SectionRestrict("SSH keys deny"),
    RestrictRule(`(deny file-read-data (subpath "~/.ssh"))`),
    RestrictRule(`(deny file-write* (subpath "~/.ssh"))`),
    SectionGrant("SSH known_hosts and config allow"),
    GrantRule(`(allow file-read* (literal "~/.ssh/known_hosts"))`),
    GrantRule(`(allow file-read* (literal "~/.ssh/config"))`),
}
```

The existing `Section()` constructor (no intent) defaults to `Setup` intent for backward compatibility. The `Comment()` constructor also defaults to `Setup`.

### Helper Functions

Updated to use correct intents:

```go
// DenyDir denies read+write to a directory tree.
func DenyDir(path string) []Rule {
    return []Rule{
        RestrictRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
        RestrictRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
    }
}

// DenyFile denies read+write to a single file.
func DenyFile(path string) []Rule {
    return []Rule{
        RestrictRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
        RestrictRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
    }
}

// AllowReadFile allows reading a single file (exception within restricted area).
func AllowReadFile(path string) Rule {
    return GrantRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
}

// EnvOverridePath returns env var value if set, otherwise defaultPath.
func EnvOverridePath(ctx *seatbelt.Context, envKey, defaultPath string) string {
    if val, ok := ctx.EnvLookup(envKey); ok && val != "" {
        return val
    }
    return defaultPath
}
```

### Guard Migration

Each guard updates its `Rules()` method to use intent-tagged constructors:

**Infrastructure guards (base, system-runtime, network, filesystem):**
All rules become `SetupRule(...)`. The `base` guard's `(deny default)` is `SetupRule`.

**Keychain, node-toolchain, nix-toolchain, git-integration:**
All rules become `GrantRule(...)` — these are allows that should survive after credential denies.

Wait: keychain and toolchain rules aren't "exceptions within restricted areas" — they're infrastructure enables. But they need to appear AFTER credential denies so they aren't overridden. Example: `git-integration` allows `~/.ssh/known_hosts`. If this is `Setup`, it appears before `ssh-keys`'s `Restrict` deny on `~/.ssh`, and the deny wins. It must be `Grant` to appear after.

Revised mapping:

| Guard | Intent | Reason |
|-------|--------|--------|
| base | Setup | foundation, must be first |
| system-runtime | Setup | infrastructure, includes refinement denies |
| network | Setup | infrastructure |
| filesystem | Setup | project/home access |
| keychain | Grant | must survive after any deny (no guard denies keychains, but Grant is defensive) |
| node-toolchain | Grant | `.npmrc` allow must survive if `npm` opt-in guard Restricts it... actually no, npm opt-in deny should WIN. So node-toolchain should be Setup? |

This reveals a tension: if `node-toolchain` is `Grant`, the `npm` opt-in guard's `Restrict` on `.npmrc` would be overridden by node-toolchain's Grant. That's wrong — the user explicitly enabled the npm guard to block `.npmrc`.

The correct model:
- **node-toolchain** is `Setup` (infrastructure). Its `.npmrc` allow appears in pass 1.
- **npm** opt-in guard is `Restrict` (credential deny). Its `.npmrc` deny appears in pass 2, AFTER node-toolchain's allow. Deny wins because it's later.
- No guard needs to Grant `.npmrc` — if the user blocks it, it stays blocked.

But then: `git-integration` allows `~/.ssh/known_hosts` as `Setup`. The `ssh-keys` guard Restricts `~/.ssh`. The Restrict comes after Setup, so the deny wins. We need `known_hosts` to be `Grant` to survive.

The rule: **a rule's intent depends on whether it needs to survive a Restrict rule.**

| Guard | Intent | Reason |
|-------|--------|--------|
| base | Setup | foundation |
| system-runtime | Setup | infrastructure (launchd refine deny also Setup) |
| network | Setup | infrastructure |
| filesystem | Setup | project/home access |
| keychain | Setup | infrastructure (no guard denies keychains) |
| node-toolchain | Setup | infrastructure (npm opt-in deny should win over this) |
| nix-toolchain | Setup | infrastructure |
| git-integration | Setup | infrastructure (`.ssh/config` and `known_hosts` are also in ssh-keys Grant) |
| ssh-keys | Deny: Restrict, Allow: Grant | deny blocks .ssh, grant re-allows known_hosts/config |
| cloud-* | Restrict | deny credential paths |
| browsers | Restrict | deny browser dirs |
| password-managers | Restrict | deny CLI stores + GPG private keys only |
| aide-secrets | Restrict | deny secrets dir |
| docker/npm/etc | Restrict | deny auth tokens |
| ClaudeAgent | Grant | agent paths must survive after all denies |
| custom guards | paths: Restrict, allowed: Grant | from config |

The `git-integration` guard allows `.ssh/config` and `.ssh/known_hosts`, but so does `ssh-keys` guard via Grant. There's overlap — but that's OK. The `ssh-keys` guard's Grant rules are the ones that matter (they come last). The `git-integration` guard's Setup allows for these same paths are harmless (overridden by ssh-keys Restrict, then re-allowed by ssh-keys Grant).

### GPG Fix

The `password-managers` guard currently denies all of `~/.gnupg` (too broad). Fix: deny only private key storage.

```go
// Before (wrong):
rules = append(rules, DenyDir(ctx.HomePath(".gnupg"))...)

// After (correct):
rules = append(rules, DenyDir(ctx.HomePath(".gnupg/private-keys-v1.d"))...)
rules = append(rules, DenyFile(ctx.HomePath(".gnupg/secring.gpg"))...)
```

GPG commit signing needs `pubring.kbx`, `trustdb.gpg`, and `gpg-agent.conf` — all of which are outside the denied paths.

### Custom Guards from YAML

Custom guards use predefined intents only:

```yaml
custom_guards:
  internal-certs:
    type: default
    paths:            # → Restrict intent
      - "~/.internal/certs"
    allowed:          # → Grant intent
      - "~/.internal/certs/ca.pem"
```

No weight or intent field is exposed in the YAML schema. `paths` always maps to Restrict, `allowed` always maps to Grant. This prevents config users from breaking the ordering guarantees.

### Testing Strategy

**Ordering tests (critical):**
- Verify Setup rules appear before Restrict rules in rendered profile
- Verify Restrict rules appear before Grant rules in rendered profile
- Verify Grant rules from ssh-keys override Restrict rules from ssh-keys
- Verify Restrict rules from npm opt-in override Setup rules from node-toolchain

**Security tests:**
- SSH known_hosts readable (Grant beats Restrict)
- SSH private keys blocked (Restrict beats Setup)
- GPG public keyring readable (not denied)
- GPG private keys blocked (Restrict)
- npm opt-in blocks `.npmrc` (Restrict beats Setup)
- Cloud credentials blocked (Restrict beats Setup)
- Keychain accessible (Setup, no Restrict targets it)

**Round-trip tests:**
- Generate full default profile, verify known_hosts in Grant section
- Generate profile with npm opt-in, verify .npmrc denied in Restrict section
- Generate profile with custom guard + allowed, verify Grant re-allow

### Files Changed

| File | Change |
|------|--------|
| `pkg/seatbelt/module.go` | Add `RuleIntent` type, constants, intent field on `Rule`, new constructors |
| `pkg/seatbelt/render.go` | Stable sort by intent before rendering |
| `pkg/seatbelt/guards/helpers.go` | `DenyDir`/`DenyFile` use `Restrict`, `AllowReadFile` uses `Grant` |
| `pkg/seatbelt/guards/guard_base.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_system_runtime.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_network.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_filesystem.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_keychain.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_node_toolchain.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_git_integration.go` | Use `SetupRule` |
| `pkg/seatbelt/guards/guard_ssh_keys.go` | Deny uses `Restrict`, allow uses `Grant` |
| `pkg/seatbelt/guards/guard_cloud.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_kubernetes.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_terraform.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_vault.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_browsers.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_password_managers.go` | Use `Restrict` via helpers + GPG fix |
| `pkg/seatbelt/guards/guard_aide_secrets.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_sensitive.go` | Use `Restrict` via helpers |
| `pkg/seatbelt/guards/guard_custom.go` | paths → `Restrict`, allowed → `Grant` |
| `pkg/seatbelt/modules/claude.go` | Use `GrantRule` |
| Test files | Intent ordering tests, security tests, round-trip tests |
