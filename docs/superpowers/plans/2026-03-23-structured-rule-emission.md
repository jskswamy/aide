# Structured Rule Emission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Enforce correct seatbelt rule ordering (Setup before Restrict before Grant) via intent-tagged rules and stable-sort rendering, fixing SSH known_hosts and GPG pubring.kbx breakage.

**Architecture:** Each `Rule` carries an explicit `RuleIntent` (Setup=100, Restrict=200, Grant=300) that determines its position in the rendered profile. The renderer collects all rules from all modules, stable-sorts by intent (preserving registration order within the same intent), and emits the profile. Helpers (`DenyDir`, `DenyFile`, `AllowReadFile`) produce rules with the correct intent automatically, so guard authors get ordering right by construction.

**Tech Stack:** Go, Apple Seatbelt (macOS sandbox)

---

## Task 1: Add RuleIntent to Rule struct + intent constructors

**Files:**
- `pkg/seatbelt/module.go`
- `pkg/seatbelt/module_test.go` (new)

### Steps

- [ ] **1.1 Write tests for intent constructors** in `pkg/seatbelt/module_test.go`
  - Test `SetupRule("(deny default)")` returns a Rule with intent `Setup` and correct lines
  - Test `RestrictRule(...)` returns a Rule with intent `Restrict` and correct lines
  - Test `GrantRule(...)` returns a Rule with intent `Grant` and correct lines
  - Test `SectionSetup("name")` returns a Rule with intent `Setup` and correct comment
  - Test `SectionRestrict("name")` returns a Rule with intent `Restrict` and correct comment
  - Test `SectionGrant("name")` returns a Rule with intent `Grant` and correct comment
  - Test that existing constructors (`Raw`, `Allow`, `Deny`, `Section`, `Comment`) produce intent `Setup` (backward compat)
  - Note: `Rule.intent` is unexported; test via a new exported `Rule.Intent() RuleIntent` accessor method

- [ ] **1.2 Add `RuleIntent` type and constants** to `pkg/seatbelt/module.go`
  ```go
  type RuleIntent int

  const (
      Setup    RuleIntent = 100
      Restrict RuleIntent = 200
      Grant    RuleIntent = 300
  )
  ```

- [ ] **1.3 Add `intent` field to `Rule` struct** in `pkg/seatbelt/module.go`
  ```go
  type Rule struct {
      intent  RuleIntent
      comment string
      lines   string
  }
  ```

- [ ] **1.4 Add `Intent()` accessor** to `Rule` in `pkg/seatbelt/module.go`
  ```go
  func (r Rule) Intent() RuleIntent { return r.intent }
  ```

- [ ] **1.5 Add intent-tagged constructors** to `pkg/seatbelt/module.go`
  ```go
  func SetupRule(text string) Rule    { return Rule{intent: Setup, lines: text} }
  func RestrictRule(text string) Rule { return Rule{intent: Restrict, lines: text} }
  func GrantRule(text string) Rule    { return Rule{intent: Grant, lines: text} }

  func SectionSetup(name string) Rule    { return Rule{intent: Setup, comment: "--- " + name + " ---"} }
  func SectionRestrict(name string) Rule { return Rule{intent: Restrict, comment: "--- " + name + " ---"} }
  func SectionGrant(name string) Rule    { return Rule{intent: Grant, comment: "--- " + name + " ---"} }
  ```

- [ ] **1.6 Update existing constructors** (`Raw`, `Allow`, `Deny`, `Section`, `Comment`) to explicitly set `intent: Setup`
  - This is technically a no-op since Go zero-value for `RuleIntent` is 0, not 100. We must set `intent: Setup` explicitly.
  ```go
  func Raw(text string) Rule     { return Rule{intent: Setup, lines: text} }
  func Allow(op string) Rule     { return Rule{intent: Setup, lines: "(allow " + op + ")"} }
  func Deny(op string) Rule      { return Rule{intent: Setup, lines: "(deny " + op + ")"} }
  func Section(name string) Rule { return Rule{intent: Setup, comment: "--- " + name + " ---"} }
  func Comment(text string) Rule { return Rule{intent: Setup, comment: text} }
  ```

- [ ] **1.7 Run tests** — `go test ./pkg/seatbelt/...` — all must pass

### Verification
- `SetupRule("x").Intent() == Setup` (100)
- `RestrictRule("x").Intent() == Restrict` (200)
- `GrantRule("x").Intent() == Grant` (300)
- `Raw("x").Intent() == Setup` (backward compat)
- `Section("x").Intent() == Setup` (backward compat)

---

## Task 2: Update renderer for intent-based sorting

**Files:**
- `pkg/seatbelt/render.go`
- `pkg/seatbelt/profile.go`
- `pkg/seatbelt/render_test.go` (new)

### Steps

- [ ] **2.1 Write tests for intent-based sorting** in `pkg/seatbelt/render_test.go`
  - Create two test modules: ModuleA returns `[RestrictRule("deny-a")]`, ModuleB returns `[GrantRule("allow-b"), SetupRule("setup-b")]`
  - Build a Profile with `Use(ModuleA, ModuleB)`, call `Render()`
  - Assert output order: `setup-b` appears before `deny-a`, `deny-a` appears before `allow-b`
  - Test that rules with the same intent preserve module registration order (ModuleA registered first, so if both emit Setup rules, ModuleA's Setup comes before ModuleB's Setup)

- [ ] **2.2 Add `taggedRule` struct** to `pkg/seatbelt/render.go`
  ```go
  type taggedRule struct {
      module string
      rule   Rule
  }
  ```

- [ ] **2.3 Add `renderTaggedRules` function** to `pkg/seatbelt/render.go`
  ```go
  func renderTaggedRules(rules []taggedRule) string {
      var b strings.Builder
      currentModule := ""
      for _, tr := range rules {
          if tr.module != currentModule {
              fmt.Fprintf(&b, "\n;; === %s ===\n", tr.module)
              currentModule = tr.module
          }
          if tr.rule.comment != "" {
              fmt.Fprintf(&b, ";; %s\n", tr.rule.comment)
          }
          if tr.rule.lines != "" {
              b.WriteString(tr.rule.lines)
              b.WriteByte('\n')
          }
      }
      return b.String()
  }
  ```
  - Note: After sorting, rules from the same module may be non-contiguous (e.g., ModuleA has Setup and Restrict rules). The module header should appear each time rules switch to a different module. This means a module may have multiple `=== name ===` headers in the output. This is acceptable and informative — it shows which module contributed rules in each intent phase.

- [ ] **2.4 Update `Profile.Render()`** in `pkg/seatbelt/profile.go`
  - Collect all rules from all modules into `[]taggedRule`
  - Stable-sort by `rule.intent`
  - Render via `renderTaggedRules`
  ```go
  func (p *Profile) Render() (string, error) {
      if len(p.modules) == 0 {
          return "", nil
      }
      var allRules []taggedRule
      for _, m := range p.modules {
          rules := m.Rules(&p.ctx)
          for _, r := range rules {
              allRules = append(allRules, taggedRule{module: m.Name(), rule: r})
          }
      }
      sort.SliceStable(allRules, func(i, j int) bool {
          return allRules[i].rule.intent < allRules[j].rule.intent
      })
      return renderTaggedRules(allRules), nil
  }
  ```
  - Add `"sort"` to imports in `profile.go`

- [ ] **2.5 Remove or keep `renderModule`** — `renderModule` and `renderRules` in `render.go` are no longer called by `Profile.Render()`. Keep them as unexported utilities (some tests may use them), or delete if unused. Check for callers first.

- [ ] **2.6 Run tests** — `go test ./pkg/seatbelt/...` — all must pass

### Verification
- Profile with mixed-intent modules renders Setup rules first, Restrict second, Grant last
- Within the same intent, module registration order is preserved

---

## Task 3: Update helpers to use correct intents

**Files:**
- `pkg/seatbelt/guards/helpers.go`
- `pkg/seatbelt/guards/helpers_test.go` (new)

### Steps

- [ ] **3.1 Write tests for helper intents** in `pkg/seatbelt/guards/helpers_test.go`
  - `DenyDir("/path")` returns 2 rules, both with `Intent() == Restrict`
  - `DenyFile("/path")` returns 2 rules, both with `Intent() == Restrict`
  - `AllowReadFile("/path")` returns 1 rule with `Intent() == Grant`
  - `DenyDir` rules contain correct deny text with `(subpath ...)`
  - `DenyFile` rules contain correct deny text with `(literal ...)`
  - `AllowReadFile` rule contains correct allow text with `(literal ...)`
  - `EnvOverridePath` and `SplitColonPaths` unchanged (existing behavior)

- [ ] **3.2 Update `DenyDir`** to use `seatbelt.RestrictRule` instead of `seatbelt.Raw`
  ```go
  func DenyDir(path string) []seatbelt.Rule {
      return []seatbelt.Rule{
          seatbelt.RestrictRule(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
          seatbelt.RestrictRule(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
      }
  }
  ```

- [ ] **3.3 Update `DenyFile`** to use `seatbelt.RestrictRule` instead of `seatbelt.Raw`
  ```go
  func DenyFile(path string) []seatbelt.Rule {
      return []seatbelt.Rule{
          seatbelt.RestrictRule(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
          seatbelt.RestrictRule(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
      }
  }
  ```

- [ ] **3.4 Update `AllowReadFile`** to use `seatbelt.GrantRule` instead of `seatbelt.Raw`
  ```go
  func AllowReadFile(path string) seatbelt.Rule {
      return seatbelt.GrantRule(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
  }
  ```

- [ ] **3.5 Run tests** — `go test ./pkg/seatbelt/guards/...` — all must pass

### Verification
- `DenyDir` rules have Restrict intent
- `DenyFile` rules have Restrict intent
- `AllowReadFile` has Grant intent
- Existing guard tests still pass (guards that use helpers now inherit correct intents automatically)

---

## Task 4: Update infrastructure guards (Setup intent)

**Files:**
- `pkg/seatbelt/guards/guard_base.go`
- `pkg/seatbelt/guards/guard_system_runtime.go`
- `pkg/seatbelt/guards/guard_network.go`
- `pkg/seatbelt/guards/guard_filesystem.go`
- `pkg/seatbelt/guards/guard_keychain.go`
- `pkg/seatbelt/guards/guard_node_toolchain.go`
- `pkg/seatbelt/guards/guard_nix_toolchain.go`
- `pkg/seatbelt/guards/guard_git_integration.go`
- `pkg/seatbelt/guards/guard_infra_intent_test.go` (new)

### Steps

- [ ] **4.1 Write tests verifying all infrastructure guard rules have Setup intent** in `pkg/seatbelt/guards/guard_infra_intent_test.go`
  - For each guard (base, system-runtime, network, filesystem, keychain, node-toolchain, nix-toolchain, git-integration):
    - Create guard, call `Rules(ctx)`, iterate all rules
    - Assert every rule has `Intent() == seatbelt.Setup`
  - Use a table-driven test with guard name and constructor

- [ ] **4.2 Update `guard_base.go`** — replace `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`
  ```go
  func (g *baseGuard) Rules(_ *seatbelt.Context) []seatbelt.Rule {
      return []seatbelt.Rule{
          seatbelt.SetupRule("(version 1)"),
          seatbelt.SetupRule("(deny default)"),
      }
  }
  ```
  - Note: `Raw` already defaults to `Setup` after Task 1.6, so this is technically redundant but makes intent explicit and self-documenting.

- [ ] **4.3 Update `guard_system_runtime.go`** — replace all `seatbelt.Raw(...)`, `seatbelt.Allow(...)`, `seatbelt.Section(...)` with `seatbelt.SetupRule(...)` and `seatbelt.SectionSetup(...)`
  - Every `seatbelt.Section("...")` becomes `seatbelt.SectionSetup("...")`
  - Every `seatbelt.Raw(...)` becomes `seatbelt.SetupRule(...)`
  - Every `seatbelt.Allow(...)` becomes `seatbelt.SetupRule("(allow ...)")` — or keep `seatbelt.Allow(...)` since it defaults to Setup after Task 1.6. Prefer explicit `SetupRule` for clarity.
  - The launchd listener deny (refinement deny within /tmp) stays as `SetupRule` — this is a refinement deny, not a credential restrict.

- [ ] **4.4 Update `guard_network.go`** — replace all `seatbelt.Allow(...)`, `seatbelt.Deny(...)`, `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`
  - `seatbelt.Allow("network*")` becomes `seatbelt.SetupRule("(allow network*)")`
  - `seatbelt.Deny("network-outbound")` becomes `seatbelt.SetupRule("(deny network-outbound)")`
  - `seatbelt.Allow("network-outbound")` becomes `seatbelt.SetupRule("(allow network-outbound)")`
  - All `seatbelt.Raw(...)` port rules become `seatbelt.SetupRule(...)`

- [ ] **4.5 Update `guard_filesystem.go`** — replace all `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`
  - In `filesystemRules()`, writable and readable rules use `seatbelt.SetupRule(...)`
  - **Important:** The `denied` paths (from `ctx.ExtraDenied`) are user-configured deny paths. These should be `seatbelt.RestrictRule(...)` since they block specific paths. Update accordingly:
    ```go
    rules = append(rules,
        seatbelt.RestrictRule(fmt.Sprintf("(deny file-read-data %s)", expr)),
        seatbelt.RestrictRule(fmt.Sprintf("(deny file-write* %s)", expr)),
    )
    ```

- [ ] **4.6 Update `guard_keychain.go`** — replace all `seatbelt.Section(...)` with `seatbelt.SectionSetup(...)`, all `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`

- [ ] **4.7 Update `guard_node_toolchain.go`** — replace all `seatbelt.Section(...)` with `seatbelt.SectionSetup(...)`, all `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`

- [ ] **4.8 Update `guard_nix_toolchain.go`** — replace all `seatbelt.Section(...)` with `seatbelt.SectionSetup(...)`, all `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`

- [ ] **4.9 Update `guard_git_integration.go`** — replace `seatbelt.Section(...)` with `seatbelt.SectionSetup(...)`, `seatbelt.Raw(...)` with `seatbelt.SetupRule(...)`

- [ ] **4.10 Run tests** — `go test ./pkg/seatbelt/guards/...` — all must pass

### Verification
- Every rule from infrastructure guards has `Intent() == Setup`
- Exception: `filesystem` guard's `ExtraDenied` rules have `Intent() == Restrict`
- Existing guard tests still pass (rendered text unchanged, only intent metadata added)

### Design decision: filesystem ExtraDenied
The `ExtraDenied` paths in the filesystem guard are user-configured deny paths (e.g., `denied: ["/sensitive"]` in YAML config). These are semantically Restrict rules — they block access to specific paths the user wants protected. They should use `RestrictRule` so they appear after Setup allows and correctly override them.

---

## Task 5: Update credential guards (Restrict + Grant intents)

**Files:**
- `pkg/seatbelt/guards/guard_ssh_keys.go`
- `pkg/seatbelt/guards/guard_cloud.go`
- `pkg/seatbelt/guards/guard_kubernetes.go`
- `pkg/seatbelt/guards/guard_terraform.go`
- `pkg/seatbelt/guards/guard_vault.go`
- `pkg/seatbelt/guards/guard_browsers.go`
- `pkg/seatbelt/guards/guard_password_managers.go`
- `pkg/seatbelt/guards/guard_credential_intent_test.go` (new)

### Steps

- [ ] **5.1 Write tests for credential guard intents** in `pkg/seatbelt/guards/guard_credential_intent_test.go`
  - **SSH keys:** deny rules have `Restrict` intent, allow rules (known_hosts, config, metadata) have `Grant` intent
  - **Cloud guards (aws, gcp, azure, digitalocean, oci):** all rules have `Restrict` intent (they only use `DenyDir`/`DenyFile` helpers, which now return `Restrict`)
  - **Kubernetes, Terraform, Vault:** all rules have `Restrict` intent (via helpers)
  - **Browsers:** all rules have `Restrict` intent (via `DenyDir` helper)
  - **Password managers:** all rules have `Restrict` intent (via helpers)
  - **aide-secrets:** all rules have `Restrict` intent (via helpers)
  - **GPG fix test:** `password-managers` guard does NOT deny `~/.gnupg` (subpath), DOES deny `~/.gnupg/private-keys-v1.d` (subpath) and `~/.gnupg/secring.gpg` (literal)
  - Section comment tests: sections in credential guards have matching intent (Restrict for deny sections, Grant for allow sections)

- [ ] **5.2 Update `guard_ssh_keys.go`** — use intent-tagged constructors
  ```go
  func (g *sshKeysGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
      home := ctx.HomeDir
      return []seatbelt.Rule{
          seatbelt.SectionRestrict("SSH keys (deny)"),
          seatbelt.RestrictRule(`(deny file-read-data
      ` + seatbelt.HomeSubpath(home, ".ssh") + `
  )`),
          seatbelt.RestrictRule(`(deny file-write*
      ` + seatbelt.HomeSubpath(home, ".ssh") + `
  )`),
          seatbelt.SectionGrant("SSH known_hosts and config (allow)"),
          seatbelt.GrantRule(`(allow file-read*
      ` + seatbelt.HomeLiteral(home, ".ssh/known_hosts") + `
      ` + seatbelt.HomeLiteral(home, ".ssh/config") + `
  )`),
          seatbelt.SectionGrant("SSH directory listing (metadata)"),
          seatbelt.GrantRule(`(allow file-read-metadata
      ` + seatbelt.HomeLiteral(home, ".ssh") + `
  )`),
      }
  }
  ```

- [ ] **5.3 Update cloud guards** — section comments become `SectionRestrict`. The `DenyDir`/`DenyFile` helper calls already produce `Restrict` intent after Task 3.
  - `guard_cloud.go`: Replace all `seatbelt.Section(...)` with `seatbelt.SectionRestrict(...)`
    - `CloudAWSGuard`: `seatbelt.SectionRestrict("AWS credentials")`
    - `CloudGCPGuard`: `seatbelt.SectionRestrict("GCP credentials")`
    - `CloudAzureGuard`: `seatbelt.SectionRestrict("Azure credentials")`
    - `CloudDigitalOceanGuard`: `seatbelt.SectionRestrict("DigitalOcean credentials")`
    - `CloudOCIGuard`: `seatbelt.SectionRestrict("OCI credentials")`

- [ ] **5.4 Update `guard_kubernetes.go`** — `seatbelt.Section(...)` becomes `seatbelt.SectionRestrict(...)`

- [ ] **5.5 Update `guard_terraform.go`** — `seatbelt.Section(...)` becomes `seatbelt.SectionRestrict(...)`

- [ ] **5.6 Update `guard_vault.go`** — `seatbelt.Section(...)` becomes `seatbelt.SectionRestrict(...)`

- [ ] **5.7 Update `guard_browsers.go`** — `seatbelt.Section(...)` becomes `seatbelt.SectionRestrict(...)`

- [ ] **5.8 Update `guard_password_managers.go`** — apply GPG fix and intent tags
  - Replace all `seatbelt.Section(...)` with `seatbelt.SectionRestrict(...)`
  - **GPG fix:** change the GPG private keys section:
    ```go
    // Before (wrong — denies all of ~/.gnupg including pubring.kbx):
    rules = append(rules, DenyDir(ctx.HomePath(".gnupg"))...)

    // After (correct — denies only private key storage):
    rules = append(rules, DenyDir(ctx.HomePath(".gnupg/private-keys-v1.d"))...)
    rules = append(rules, DenyFile(ctx.HomePath(".gnupg/secring.gpg"))...)
    ```
  - Update `aide-secrets` guard section: `seatbelt.SectionRestrict("aide secrets")`

- [ ] **5.9 Run tests** — `go test ./pkg/seatbelt/guards/...` — all must pass

### Verification
- SSH deny rules: `Restrict` intent; SSH allow rules: `Grant` intent
- All cloud/k8s/terraform/vault/browser/password-manager deny rules: `Restrict` intent
- GPG: `~/.gnupg/private-keys-v1.d` denied, `~/.gnupg/secring.gpg` denied, `~/.gnupg/pubring.kbx` NOT denied
- Section comments match their rules' intents

---

## Task 6: Update opt-in guards + custom guards + ClaudeAgent

**Files:**
- `pkg/seatbelt/guards/guard_sensitive.go`
- `pkg/seatbelt/guards/guard_custom.go`
- `pkg/seatbelt/modules/claude.go`
- `pkg/seatbelt/guards/guard_optin_intent_test.go` (new)
- `pkg/seatbelt/modules/claude_test.go` (new)

### Steps

- [ ] **6.1 Write tests for opt-in guard intents** in `pkg/seatbelt/guards/guard_optin_intent_test.go`
  - **docker, github-cli, npm, netrc, vercel:** all rules have `Restrict` intent (via `DenyDir`/`DenyFile` helpers), section comments have `Restrict` intent
  - **Custom guard:** `paths` rules have `Restrict` intent, `allowed` rules have `Grant` intent

- [ ] **6.2 Write tests for ClaudeAgent intents** in `pkg/seatbelt/modules/claude_test.go`
  - All rules from `ClaudeAgent()` have `Grant` intent
  - Section comments have `Grant` intent

- [ ] **6.3 Update `guard_sensitive.go`** — replace all `seatbelt.Section(...)` with `seatbelt.SectionRestrict(...)`
  - Docker: `seatbelt.SectionRestrict("Docker credentials")`
  - GitHub CLI: `seatbelt.SectionRestrict("GitHub CLI credentials")`
  - npm: `seatbelt.SectionRestrict("npm/yarn credentials")`
  - netrc: `seatbelt.SectionRestrict("netrc credentials")`
  - Vercel: `seatbelt.SectionRestrict("Vercel credentials")`
  - The `DenyDir`/`DenyFile` helper calls already produce `Restrict` intent after Task 3.

- [ ] **6.4 Update `guard_custom.go`** — use intent-tagged constructors
  - Deny rules: replace `seatbelt.Raw(...)` with `seatbelt.RestrictRule(...)`
    ```go
    rules = append(rules,
        seatbelt.RestrictRule(`(deny file-read-data
        `+`(subpath "`+p+`")`+`
    )`),
        seatbelt.RestrictRule(`(deny file-write*
        `+`(subpath "`+p+`")`+`
    )`),
    )
    ```
  - Allow rules: replace `seatbelt.Raw(...)` with `seatbelt.GrantRule(...)`
    ```go
    rules = append(rules,
        seatbelt.GrantRule(`(allow file-read*
        `+`(literal "`+filepath.Clean(expanded)+`")`+`
    )`),
    )
    ```

- [ ] **6.5 Update `modules/claude.go`** — replace all `seatbelt.Section(...)` with `seatbelt.SectionGrant(...)`, all `seatbelt.Raw(...)` with `seatbelt.GrantRule(...)`
  - `seatbelt.SectionGrant("Claude user data")`
  - `seatbelt.GrantRule(...)` for the read-write rule
  - `seatbelt.SectionGrant("Claude managed configuration")`
  - `seatbelt.GrantRule(...)` for the read-only rule

- [ ] **6.6 Run tests** — `go test ./pkg/seatbelt/...` — all must pass

### Verification
- Opt-in guards: all rules `Restrict` intent
- Custom guard: deny rules `Restrict`, allow rules `Grant`
- ClaudeAgent: all rules `Grant` intent

---

## Task 7: Integration tests -- ordering guarantees

**Files:**
- `pkg/seatbelt/integration_test.go` (new)

### Steps

- [ ] **7.1 Write integration test: full profile ordering**
  - Register guards in this order: base, system-runtime, network, filesystem, keychain, node-toolchain, nix-toolchain, git-integration, ssh-keys, cloud-aws, browsers, password-managers, aide-secrets, ClaudeAgent
  - Render profile
  - Assert: all `(deny default)` (Setup) appears before `(deny file-read-data (subpath ".../.ssh"))` (Restrict)
  - Assert: all Restrict rules appear before Claude Agent paths (Grant)

- [ ] **7.2 Write integration test: SSH known_hosts survives**
  - Register: base, filesystem, ssh-keys
  - Render profile
  - Find the line index of `deny file-read-data` with `.ssh` subpath (Restrict)
  - Find the line index of `allow file-read*` with `.ssh/known_hosts` literal (Grant)
  - Assert: Grant line index > Restrict line index (last-rule-wins means Grant wins)

- [ ] **7.3 Write integration test: npm opt-in overrides node-toolchain**
  - Register: base, node-toolchain, npm (opt-in)
  - Render profile
  - Find line index of `allow ... .npmrc` (Setup, from node-toolchain)
  - Find line index of `deny ... .npmrc` (Restrict, from npm)
  - Assert: Restrict line index > Setup line index (deny wins because it's later)

- [ ] **7.4 Write integration test: GPG pubring.kbx not denied**
  - Register: base, filesystem, password-managers
  - Render profile
  - Assert: output does NOT contain `(subpath ".../.gnupg")` — the too-broad deny is gone
  - Assert: output contains `(subpath ".../.gnupg/private-keys-v1.d")` — private keys denied
  - Assert: output contains `(literal ".../.gnupg/secring.gpg")` — legacy private key denied
  - Assert: output does NOT contain `pubring.kbx` or `trustdb.gpg` — these are not mentioned (not denied)

- [ ] **7.5 Write integration test: custom guard paths/allowed ordering**
  - Create custom guard with `Paths: ["~/.internal/certs"]`, `Allowed: ["~/.internal/certs/ca.pem"]`
  - Register: base, filesystem, custom
  - Render profile
  - Find line index of deny for `.internal/certs` (Restrict)
  - Find line index of allow for `.internal/certs/ca.pem` (Grant)
  - Assert: Grant line index > Restrict line index

- [ ] **7.6 Write integration test: round-trip with default guards**
  - Use the same guard set that the main `aide` binary uses (all always + all default guards)
  - Render profile
  - Verify the output is valid (non-empty, starts with `(version 1)`, contains `(deny default)`)
  - Verify SSH known_hosts allow is after SSH deny
  - Verify ClaudeAgent rules are last

- [ ] **7.7 Run full test suite** — `go test ./pkg/seatbelt/...` — all must pass

### Verification
- All ordering guarantees hold across realistic guard combinations
- No regression in existing guard behavior
- GPG pubring.kbx is accessible for commit signing

---

## Task 8: Commit spec + plan

### Steps

- [ ] **8.1 Run full test suite one final time** — `go test ./pkg/seatbelt/...`
- [ ] **8.2 Run `go vet ./pkg/seatbelt/...`** — no warnings
- [ ] **8.3 Commit all changes** using `/commit`

---

## File Change Summary

| File | Change | Task |
|------|--------|------|
| `pkg/seatbelt/module.go` | Add `RuleIntent`, intent field, constructors, accessor | 1 |
| `pkg/seatbelt/module_test.go` | Intent constructor tests | 1 |
| `pkg/seatbelt/render.go` | Add `taggedRule`, `renderTaggedRules` | 2 |
| `pkg/seatbelt/profile.go` | Rewrite `Render()` with stable sort | 2 |
| `pkg/seatbelt/render_test.go` | Renderer ordering tests | 2 |
| `pkg/seatbelt/guards/helpers.go` | `DenyDir`/`DenyFile` use `RestrictRule`, `AllowReadFile` uses `GrantRule` | 3 |
| `pkg/seatbelt/guards/helpers_test.go` | Helper intent tests | 3 |
| `pkg/seatbelt/guards/guard_base.go` | `SetupRule` | 4 |
| `pkg/seatbelt/guards/guard_system_runtime.go` | `SetupRule`, `SectionSetup` | 4 |
| `pkg/seatbelt/guards/guard_network.go` | `SetupRule` | 4 |
| `pkg/seatbelt/guards/guard_filesystem.go` | `SetupRule` for allows, `RestrictRule` for `ExtraDenied` | 4 |
| `pkg/seatbelt/guards/guard_keychain.go` | `SetupRule`, `SectionSetup` | 4 |
| `pkg/seatbelt/guards/guard_node_toolchain.go` | `SetupRule`, `SectionSetup` | 4 |
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | `SetupRule`, `SectionSetup` | 4 |
| `pkg/seatbelt/guards/guard_git_integration.go` | `SetupRule`, `SectionSetup` | 4 |
| `pkg/seatbelt/guards/guard_infra_intent_test.go` | Infrastructure intent tests | 4 |
| `pkg/seatbelt/guards/guard_ssh_keys.go` | `RestrictRule` for deny, `GrantRule` for allow, intent sections | 5 |
| `pkg/seatbelt/guards/guard_cloud.go` | `SectionRestrict` | 5 |
| `pkg/seatbelt/guards/guard_kubernetes.go` | `SectionRestrict` | 5 |
| `pkg/seatbelt/guards/guard_terraform.go` | `SectionRestrict` | 5 |
| `pkg/seatbelt/guards/guard_vault.go` | `SectionRestrict` | 5 |
| `pkg/seatbelt/guards/guard_browsers.go` | `SectionRestrict` | 5 |
| `pkg/seatbelt/guards/guard_password_managers.go` | `SectionRestrict`, GPG fix | 5 |
| `pkg/seatbelt/guards/guard_credential_intent_test.go` | Credential guard intent tests | 5 |
| `pkg/seatbelt/guards/guard_sensitive.go` | `SectionRestrict` | 6 |
| `pkg/seatbelt/guards/guard_custom.go` | `RestrictRule` for deny, `GrantRule` for allow | 6 |
| `pkg/seatbelt/modules/claude.go` | `GrantRule`, `SectionGrant` | 6 |
| `pkg/seatbelt/guards/guard_optin_intent_test.go` | Opt-in guard intent tests | 6 |
| `pkg/seatbelt/modules/claude_test.go` | ClaudeAgent intent tests | 6 |
| `pkg/seatbelt/integration_test.go` | End-to-end ordering and security tests | 7 |

## Critical Invariants

1. **Setup < Restrict < Grant** in rendered output, always
2. **Stable sort** preserves module registration order within the same intent
3. **Helpers encode intent** — guard authors using `DenyDir`/`DenyFile`/`AllowReadFile` get correct intents automatically
4. **Backward compatibility** — `Raw`, `Allow`, `Deny`, `Section`, `Comment` default to `Setup`
5. **GPG fix** — deny `~/.gnupg/private-keys-v1.d` and `~/.gnupg/secring.gpg` only, not all of `~/.gnupg`
6. **Custom guards** — `paths` always Restrict, `allowed` always Grant (no user-facing intent field)
