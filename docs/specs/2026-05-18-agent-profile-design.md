# First-class Agent Profile Support — Design

**Status:** Approved (brainstorming complete 2026-05-18)
**Tracks:** AIDE-m6i
**Out of scope deps:** AIDE-fxc (project-scope `.aide.yaml` merging — see "Scope" below)

## Goal

Let users declare a profile name for an agent context and have the
driver compute the right config-dir env var, instead of hand-rolling
agent-specific env vars (`CLAUDE_CONFIG_DIR`, `GEMINI_HOME`,
`CODEX_HOME`, `COPILOT_HOME`, …) per context.

## Motivation

Today, users declaring a multi-profile context have to know each
agent's internal env-var name and the convention for naming the
config directory. This leaks agent internals into user config and
caused real bugs:

- **AIDE-brx** — tilde-expansion silently broke sandbox subpath rules
  when the env value was `~/.claude-firmus` (literal, not absolute).
- Recent v2 work surfaced multi-profile failures because `aide`
  didn't pass env through correctly into sync/adopt/list paths.

The seatbelt module *already* knows the mapping
(`resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", …)` is literally that).
Invert the relationship: config declares a profile name, the driver
computes env var + path. Same data, no user-facing knowledge of
agent internals.

## Schema

Two new optional fields on `config.Context`:

```yaml
contexts:
  firmus:
    agent: claude
    profile: firmus                    # → CLAUDE_CONFIG_DIR=~/.claude-firmus

  custom:
    agent: claude
    profile: dev
    profile_dir: /opt/claude-dev       # explicit override of derived path

  legacy:
    agent: claude
    env:
      CLAUDE_CONFIG_DIR: ~/.claude-old # explicit env still works (backward compat)
```

**Default path derivation:** `~/.<agent>-<profile>` joined against
`homeDir`. Example: `agent: claude` + `profile: firmus` →
`~/.claude-firmus` (expanded to absolute path at resolve time).

**`profile_dir`** is an optional override. When set, the driver uses
it verbatim (tilde-expanded) instead of the derived path.

**Validation rules** (all enforced at `config.Load` time):

| Rule | Sentinel error | Trigger |
|---|---|---|
| `profile` matches `^[a-zA-Z0-9_-]{1,64}$` | `ErrProfileNameInvalid` | bad chars, empty, too long |
| `profile_dir` requires `profile` to be set | `ErrProfileNameInvalid` | `profile_dir` without `profile` |
| Driver supports profile (`Caps.ProfileEnvKey != ""`) | `ErrProfileNotSupported` | e.g. cursor-agent |
| Resolved path is under `$HOME` | `ErrProfileDirOutsideHome` | `profile_dir: /etc/foo` |
| `profile` and the agent's env var in `env:` block are both declared | `ErrProfileConflict` | duplicate declaration |
| `profile` / `profile_dir` declared in `.aide.yaml` | `ErrProfileNotProjectScoped` | project override |

The "not allowed in `.aide.yaml`" rule is intentional: `profile:` is
fundamentally about *which directory under MY HOME does MY agent
state live in* — that's a user-side decision, not a project-side
one. Other `.aide.yaml` fields make sense at project scope (sandbox,
MCP, agent choice); `profile:` does not.

## Driver interface

### `provision.Capabilities`

Add one field:

```go
type Capabilities struct {
    AgentName       string
    SupportsPlugins bool
    SupportsMCP     bool
    RequiresTTY     bool
    SourceShapes    []SourceShape
    ProfileEnvKey   string  // NEW: e.g. "CLAUDE_CONFIG_DIR"
                            // empty string = no profile support
}
```

### `DriverBase.Profile`

```go
// ErrProfileNotSupported wraps when an agent driver returns
// Caps.ProfileEnvKey == "". Callers use errors.Is() to branch.
var ErrProfileNotSupported = errors.New("profile not supported by agent")

// Profile resolves the env var name and absolute path for a profile.
// override is non-empty when the user supplied profile_dir.
func (d DriverBase) Profile(name, override, homeDir string) (envKey, absPath string, err error) {
    if d.Caps.ProfileEnvKey == "" {
        return "", "", fmt.Errorf(
            "%w: %q (env var does not isolate the full config tree)",
            ErrProfileNotSupported, d.Caps.AgentName,
        )
    }
    if override != "" {
        return d.Caps.ProfileEnvKey, homepath.Expand(override, homeDir), nil
    }
    dirName := fmt.Sprintf(".%s-%s", d.Caps.AgentName, name)
    return d.Caps.ProfileEnvKey, filepath.Join(homeDir, dirName), nil
}
```

### Per-driver `ProfileEnvKey` (populated in each driver's `New()`)

| Driver | ProfileEnvKey | Notes |
|---|---|---|
| `claude` | `CLAUDE_CONFIG_DIR` | covers full claude config tree |
| `gemini` | `GEMINI_HOME` | covers full gemini config tree |
| `codex` | `CODEX_HOME` | covers full codex config tree |
| `copilot` | `COPILOT_HOME` | covers full copilot config tree |
| `cursor` | `""` | `CURSOR_CONFIG_DIR` only covers `cli-config.json`, not mcp.json. Explicit non-support. |

When new drivers land (Goose `GOOSE_PATH_ROOT`, Amp `AMP_HOME`,
Aider TBD), they populate the field at the same spot.

### Where env is injected

In `provision.ResolveContext` — the single chokepoint every
launcher/sync/adopt/list call already goes through. Pseudocode:

```go
func ResolveContext(name string, ctx config.Context, homeDir, projectRoot string, env map[string]string) (Context, error) {
    out := Context{Name: name, Agent: ctx.Agent, HomeDir: homeDir, ProjectRoot: projectRoot, Env: maps.Clone(env)}
    if ctx.Profile != "" {
        driver := LookupProvisioner(ctx.Agent)
        if driver == nil {
            return out, fmt.Errorf("no provisioner registered for agent %q", ctx.Agent)
        }
        envKey, absPath, err := driver.Profile(ctx.Profile, ctx.ProfileDir, homeDir)
        if err != nil {
            return out, err
        }
        if out.Env == nil {
            out.Env = map[string]string{}
        }
        out.Env[envKey] = absPath
    }
    return out, nil
}
```

The existing seatbelt module's `resolveConfigDirs` reads `ctx.Env`
at sandbox-rule emission time — no change there, it just sees the
injected value.

## Validation helper

Lives in `internal/config/profile.go`:

```go
var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// validateProfile runs all profile-related checks for one context.
// driver is looked up via the provisioner registry; nil = skip the
// supports-profile check (config-loader still catches charset etc.).
func validateProfile(ctx config.Context, driver provision.Provisioner, homeDir string, fromProjectOverride bool) error {
    if fromProjectOverride && (ctx.Profile != "" || ctx.ProfileDir != "") {
        return fmt.Errorf("%w: .aide.yaml may not set profile/profile_dir", provision.ErrProfileNotProjectScoped)
    }
    if ctx.Profile == "" {
        if ctx.ProfileDir != "" {
            return fmt.Errorf("%w: profile_dir requires profile to be set", provision.ErrProfileNameInvalid)
        }
        return nil
    }
    if !validProfileName.MatchString(ctx.Profile) {
        return fmt.Errorf("%w: %q (allowed: [a-zA-Z0-9_-]+, max 64 chars)",
            provision.ErrProfileNameInvalid, ctx.Profile)
    }
    envKey, absPath, err := driver.Profile(ctx.Profile, ctx.ProfileDir, homeDir)
    if err != nil {
        return err
    }
    if !strings.HasPrefix(absPath, homeDir+string(filepath.Separator)) {
        return fmt.Errorf("%w: %q resolves to %q",
            provision.ErrProfileDirOutsideHome, ctx.ProfileDir, absPath)
    }
    if _, ok := ctx.Env[envKey]; ok {
        return fmt.Errorf("%w: both `profile: %s` and env.%s declared (pick one)",
            provision.ErrProfileConflict, ctx.Profile, envKey)
    }
    return nil
}
```

Called from `config.Load` for each context, after the driver
registry has been populated.

## Sandbox interaction

No new code needed. The seatbelt module's `resolveConfigDirs` (in
`pkg/seatbelt/modules/helpers.go`) already reads the env var:

```go
configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
    filepath.Join(home, ".claude"),
})
```

When `Profile != ""` injects `CLAUDE_CONFIG_DIR=/abs/path` into the
context's env, `resolveConfigDirs` sees it and emits a subpath rule
on the absolute path (already tilde-expanded by `ResolveContext`).

Existing logic stays untouched; the change is upstream of it.

## Backward compatibility

Existing configs unchanged. Three scenarios:

| User config | Behavior |
|---|---|
| No `profile:` and no env override | Default agent dir (`~/.claude` etc.) — unchanged |
| No `profile:`, explicit `env: { CLAUDE_CONFIG_DIR: … }` | Driver honors the explicit env — unchanged |
| `profile: foo` | Driver computes `CLAUDE_CONFIG_DIR=~/.claude-foo`, injects into env |
| Both `profile: foo` and `env: { CLAUDE_CONFIG_DIR: … }` | `ErrProfileConflict` at load |
| `profile: foo` on cursor-agent | `ErrProfileNotSupported` at load |

No migration step required; new users get the cleaner abstraction.

## Scope

**In scope:**
- New schema fields + their validation
- `Capabilities.ProfileEnvKey` and `DriverBase.Profile()` method
- Env injection in `ResolveContext`
- Per-driver `ProfileEnvKey` population for claude / gemini / codex / copilot
- Cursor's explicit non-support (driver returns `""`)
- Release notes entry

**Out of scope:**
- `profile:` in `.aide.yaml` (project override) — explicitly rejected
  via `ErrProfileNotProjectScoped`. Owner is the user, not the project.
- Goose/Amp/Aider drivers — those agents don't have provision drivers
  yet; when they do, populating `ProfileEnvKey` is a one-line addition.
- `aide context create` wizard offering `profile:` as a default —
  ergonomic improvement, separable.
- Path-collision detection ("two contexts both resolve to the same
  profile dir") — user-config issue, not engine issue, YAGNI.
- Adopting a validation library — see AIDE-p5k.

## Implementation order

1. Define sentinel errors in `internal/provision/errors.go` (or alongside `Capabilities` in `provisioner.go`).
2. Add `ProfileEnvKey` to `Capabilities`; populate in each driver's `New()`.
3. Implement `DriverBase.Profile()`.
4. Driver tests: 4 supported drivers happy path + override + cursor's error.
5. Add `Profile`, `ProfileDir` fields to `config.Context` with YAML tags.
6. Implement `validateProfile` helper in `internal/config/profile.go`.
7. Wire `validateProfile` into `config.Load` (one call per context).
8. Update `ResolveContext` to inject env when `Profile != ""`.
9. End-to-end test: config with `profile: foo` → launcher banner shows `CLAUDE_CONFIG_DIR=/abs/path` and seatbelt rule on that absolute path.
10. Update `docs/specs/2026-05-15-declarative-agent-provisioning-design.md` — remove the `profile:` follow-up from "Out of scope".
11. Release notes entry under `### ✨ New`.

## Release notes draft

```markdown
### ✨ New

- **First-class agent profile support.** Multi-profile contexts can
  now declare `profile: <name>` instead of hand-rolling agent-specific
  env vars. The driver computes the right env var and absolute config
  path; users don't need to know `CLAUDE_CONFIG_DIR` vs `GEMINI_HOME`
  vs `CODEX_HOME` vs `COPILOT_HOME`. Optional `profile_dir` overrides
  the derived `~/.<agent>-<name>` path. Cursor-agent is intentionally
  not supported — its env var doesn't isolate MCP config; use a
  project-scope `.cursor/mcp.json` for per-project MCP instead.
  Existing configs with explicit env vars keep working unchanged.
```

## Test plan

- **Driver tests** (`internal/provision/agents/{claude,gemini,codex,copilot}/profile_test.go`):
  - happy path: `driver.Profile("foo", "", "/h")` returns expected env key + `/h/.<agent>-foo`
  - override: `driver.Profile("foo", "~/custom", "/h")` returns expected env key + `/h/custom` (tilde-expanded)
- **Cursor driver** (`internal/provision/agents/cursor/profile_test.go` once cursor driver exists, OR in cursor's seatbelt module for now): `Profile` returns `errors.Is(err, ErrProfileNotSupported)`.
- **Validation** (`internal/config/profile_test.go`):
  - rejects empty profile / bad chars / over-length
  - rejects `profile_dir` without `profile`
  - rejects `profile_dir` outside HOME
  - rejects both `profile` and explicit env-var-in-env
  - rejects `profile` from `.aide.yaml`
- **ResolveContext** (`internal/provision/resolve_test.go`): with `Profile: "foo"`, env contains `CLAUDE_CONFIG_DIR=/abs/path`.
- **End-to-end** (`cmd/aide/...`): `aide which` for a profile-using context shows the resolved env var, and the launched banner has a seatbelt subpath rule on the absolute path.
