# Banner Template Engine & Extra Path Transparency

**Date:** 2026-03-25
**Status:** Approved

## Problem

1. The banner uses imperative `fmt.Fprintf` rendering across three style functions (~300 lines of duplicated layout logic). Adding a new style or section requires modifying Go code in multiple places.
2. Extra sandbox paths (`writable_extra`, `readable_extra`, `denied`) from config are invisible in the banner — they silently widen or narrow the sandbox without the user seeing it.

## Goals

- Replace `fmt.Fprintf` banner rendering with `text/template` + `embed.FS` templates
- Show non-capability extra paths (writable, readable, denied) in the banner
- Make adding new banner styles or sections a template-only change
- Improve test ergonomics with structural assertions instead of golden strings

## Non-Goals

- Replacing `fatih/color` with a different styling library
- Adding new banner styles beyond compact/boxed/clean
- Changing the `RenderBanner(w, style, data)` public API signature

## Design

### Data Model Changes

`BannerData` gets three new fields for paths that come from sandbox config but NOT from capabilities:

```go
type BannerData struct {
    // ... existing fields ...
    ExtraWritable []string // writable_extra paths not from capabilities
    ExtraReadable []string // readable_extra paths not from capabilities
    ExtraDenied   []string // denied/denied_extra paths not from capabilities
}
```

**Population logic** in `launcher.buildBannerData()`: The original config paths must be captured **before** capability overrides are merged into `sandboxCfg` (launcher.go line 248). In `Launch()`, snapshot the original config paths before the merge:

```go
// Capture original config paths before capability merge
var configWritable, configReadable, configDenied []string
if sandboxCfg != nil {
    configWritable = append([]string{}, sandboxCfg.WritableExtra...)
    configReadable = append([]string{}, sandboxCfg.ReadableExtra...)
    configDenied = append([]string{}, sandboxCfg.DeniedExtra...)
}
// ... then capability overrides are appended into sandboxCfg ...
```

Then in `buildBannerData`, set-difference: `configWritable` minus `capOverrides.WritableExtra` → `ExtraWritable` (and same for readable/denied).

### File Structure

```
internal/ui/
├── banner.go          # Template engine: embed, parse, RenderBanner()
├── banner_test.go     # Structural assertions against template output
├── funcmap.go         # Color FuncMap helpers + data accessor funcs
├── types.go           # BannerData, SandboxInfo, CapabilityDisplay, etc.
├── templates/
│   ├── compact.tmpl
│   ├── boxed.tmpl
│   └── clean.tmpl
```

### Template Engine (`banner.go`)

```go
//go:embed templates/*.tmpl
var templateFS embed.FS

func RenderBanner(w io.Writer, style string, data *BannerData) error {
    tmpl, err := template.New("").Funcs(colorFuncMap()).ParseFS(templateFS, "templates/*.tmpl")
    if err != nil {
        return fmt.Errorf("parsing banner templates: %w", err)
    }
    if err := tmpl.ExecuteTemplate(w, style+".tmpl", data); err != nil {
        return fmt.Errorf("rendering banner style %q: %w", style, err)
    }
    return nil
}
```

- Templates embedded via `//go:embed templates/*.tmpl`
- Single `RenderBanner` entry point replaces three separate render functions
- `RenderCompact`, `RenderBoxed`, `RenderClean` are **removed** (previously exported but only called via `RenderBanner` switch)
- Style selection via template name lookup
- Errors returned explicitly (template execution can fail at runtime unlike `Fprintf`)

### FuncMap (`funcmap.go`)

Color helpers — wrap `fatih/color`, return `string` (not write to `io.Writer`):

| Function | Description |
|---|---|
| `bold(s)` | Bold text |
| `green(s)` | Green text |
| `boldGreen(s)` | Bold + green |
| `yellow(s)` | Yellow text |
| `dim(s)` | Faint/dim text |
| `red(s)` | Bold red text |
| `cyan(s)` | Cyan text |

Data helpers — keep templates declarative. Functions marked *existing* wrap current code; *new* are created for this change:

| Function | Description | Status |
|---|---|---|
| `agentDisplay(d)` | Agent name + path display | *existing* — wraps `agentDisplay()` |
| `secretDisplay(d)` | Secret name + key count | *existing* — wraps `secretDisplay()` |
| `envLines(d)` | Formatted env variable lines | *existing* — wraps `envLines()` |
| `networkLabel(d)` | Network mode display string | *existing* — wraps `sandboxNetworkLabel()` (renamed) |
| `truncate(items, n)` | Cap list at n items with "(+N more)" | *existing* — wraps `truncateList()` |
| `join(items, sep)` | `strings.Join` | *new* — stdlib passthrough |
| `hasItems(s)` | `len(s) > 0` | *new* |
| `first(s)` | First element of `[]string`, empty string if empty | *new* |
| `rest(s)` | All elements after first of `[]string`, nil if <=1 | *new* |
| `slice(s, i)` | `s[i:]` for `[]string` — for idiomatic `range` | *new* |
| `sandboxDisabled(d)` | Nil-safe check: `d.Sandbox != nil && d.Sandbox.Disabled` | *new* |
| `hasExtraPaths(d)` | True if any of ExtraWritable/ExtraReadable/ExtraDenied non-empty | *new* |
| `hasCapOrExtra(d)` | True if capabilities OR extra paths present (vs code-only) | *new* |

### Template Structure (compact.tmpl)

```
{{boldGreen "🔧 aide"}}{{if .ContextName}} · {{.ContextName}}{{end}} ({{agentDisplay .}})
{{- if .MatchReason}}
   📁 {{.MatchReason}}
{{- end}}
{{- if .SecretName}}
   🔐 secret: {{secretDisplay .}}
{{- end}}
{{- $lines := envLines . -}}
{{- if $lines}}
   📦 env: {{index $lines 0}}
   {{- range slice $lines 1}}
          {{.}}
   {{- end}}
{{- end}}
{{- if sandboxDisabled .}}
   🛡 sandbox: disabled
{{- else if hasCapOrExtra .}}
   🛡 sandbox: network {{networkLabel .}}
   {{- if and .Sandbox .Sandbox.Ports (ne .Sandbox.Ports "all")}}
   🛡 ports: {{.Sandbox.Ports}}
   {{- end}}
   {{- range .Capabilities}}
     {{boldGreen "✓"}} {{printf "%-10s" .Name}} {{truncate .Paths 3}}
     {{- if and .Source (ne .Source "context config")}}  {{dim (printf "← %s" .Source)}}{{end}}
   {{- end}}
   {{- if .ExtraWritable}}
     {{yellow "⊕"}} writable:  {{truncate .ExtraWritable 3}}
   {{- end}}
   {{- if .ExtraReadable}}
     {{yellow "⊕"}} readable:  {{truncate .ExtraReadable 3}}
   {{- end}}
   {{- if .ExtraDenied}}
     {{red "⊘"}} denied:    {{truncate .ExtraDenied 3}}
   {{- end}}
   {{- range .DisabledCaps}}
     {{dim "○"}} {{printf "%-10s" .Name}} disabled for this session  ← --without
   {{- end}}
   {{- range .NeverAllow}}
     {{red "✗"}} denied    {{.}} (never-allow)
   {{- end}}
   {{- if .CredWarnings}}
     {{yellow "⚠"}} credentials exposed: {{join .CredWarnings ", "}}
   {{- end}}
   {{- range .CompWarnings}}
     {{yellow "⚠"}} {{.}}
   {{- end}}
{{- else}}
   🛡 sandbox: network {{networkLabel .}}, code-only
   {{- if and .Sandbox .Sandbox.Ports (ne .Sandbox.Ports "all")}}
   🛡 ports: {{.Sandbox.Ports}}
   {{- end}}
{{- end}}
{{- range .Warnings}}
     {{yellow "⚠"}} {{.}}
{{- end}}
{{- if .AutoApprove}}
   {{red "⚡ AUTO-APPROVE — all agent actions execute without confirmation"}}
{{- end}}
```

**Boxed template** follows the same conditional logic but wraps lines with `│ ` prefix, uses `┌─ aide ──...` / `└──...` borders, and adds labeled rows (`🎯 Context`, `🤖 Agent`) with `cyan` labels. It must include the `sandboxDisabled` / `hasCapOrExtra` / `code-only` branches — the current imperative boxed code is missing the disabled check which is a bug to fix.

**Clean template** uses no emoji, indented `cyan` labels (`Agent`, `Matched`, `Secret`, `Env`, `sandbox:`), same conditional logic.

Extra paths use `⊕` (writable/readable) and `⊘` (denied) to visually distinguish from capability `✓` lines.

### Package Boundaries

| Package | Role | Changes |
|---|---|---|
| `ui` | Pure presentation | Templates, FuncMap, types extraction |
| `launcher` | Orchestration | Populate `ExtraWritable`/`ExtraReadable`/`ExtraDenied` in `buildBannerData` |
| `sandbox` | Policy building | None — already exposes `Policy.ExtraWritable` etc. |
| `capability` | Cap resolution | None — `SandboxOverrides` already has the paths |
| `config` | Schema | None |

The path diffing logic (config paths minus capability paths) lives in `launcher.buildBannerData()` as extracted helpers. `ui` receives pre-computed data only.

### Testing Strategy

- Set `color.NoColor = true` to strip ANSI
- Render to `bytes.Buffer` with known `BannerData`
- **Structural assertions**: verify sections present/absent based on data fields
  - ExtraWritable populated → output contains "writable:" line
  - ExtraWritable empty → no "writable:" line
  - Capabilities present → each cap name appears
- **Contains/not-contains**: check specific paths appear in output
- No hardcoded golden strings — resilient to formatting tweaks

### Migration

Clean replacement, no parallel systems:

1. Extract types to `types.go`
2. Create `funcmap.go`
3. Write three `.tmpl` files
4. Rewrite `banner.go` with template engine
5. Remove exported `RenderCompact`, `RenderBoxed`, `RenderClean` functions and package-level color vars (`boldGreen`, `cyan`, `yellow`, `dim`, `red`). These are only called through the `RenderBanner` switch — no external callers.
6. Snapshot original config paths in `Launch()` before capability merge (line 248), pass to `buildBannerData`
7. Update `buildBannerData` to populate `ExtraWritable`/`ExtraReadable`/`ExtraDenied` via set-difference
8. Update tests to structural assertions

`renderGuardSection` stays (used by `aide sandbox guards` CLI) — not part of the banner template migration. It retains its own color vars if needed.

### What the Banner Looks Like After

For the `tailsctl` context:

```
🔧 aide · tailsctl (claude)
   📁 /Users/subramk/source/github.com/tails-mpt/tailsctl
   🔐 secret: work
   📦 env: ANTHROPIC_API_KEY ← secrets.anhropic_api_key
          CLAUDE_CONFIG_DIR ← literal
   🛡 sandbox: network unrestricted
     ⊕ writable:  ~/.config/gcloud, ~/.kube/
     ⊕ readable:  ~/.docker
```

For the `work` context:

```
🔧 aide · work (claude)
   📁 ~/source/github.com/twlabs/**
   🔐 secret: work
   📦 env: ANTHROPIC_API_KEY ← secrets.anhropic_api_key
          CLAUDE_CONFIG_DIR ← literal
   🛡 sandbox: network outbound only
     ⊕ writable:  ~/.azure/, ~/.kube/, ~/source/.../firmus (+1 more)
```
