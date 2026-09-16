# `--format json` for Read/Query Commands

## Problem

Every `aide` command today prints for humans. Most of the 24 read/query
commands (`which`, `cap list`, `sandbox list`, `agents list`, `env
list`, `secrets list`, etc.) build their output inline with
`fmt.Fprintf` loops — see `capListCmd` (`cmd/aide/cap.go:117-200`),
which sorts capability names and prints a fixed-width table row per
capability with no intermediate data structure. There is no way to
consume `aide`'s state programmatically (a script, another tool, an
agent) without scraping formatted text.

One precedent already exists: `aide explain` (`cmd/aide/explain.go`)
has its own local `--format human|agent|json` flag and builds a typed
`explain.Document` (`internal/explain/model.go`) that `explain.RenderJSON`
(`internal/explain/render_json.go`) marshals directly. `aide which`
(`cmd/aide/status.go:190`) also already builds a typed struct,
`ui.BannerData`, though today only for template rendering, not JSON.
Everything else has no typed data behind its output.

## Goals

- Every read/query command supports `--format json`, emitting a single
  JSON value on success that round-trips the same information the
  human-mode output shows.
- One consistent flag, `--format`, works identically across all
  commands (`human` default, `json`), wired once as a persistent flag
  on `rootCmd` rather than copy-pasted per command.
- Errors under `--format json` are also machine-parseable — never a
  human sentence mixed into stdout/stderr that a JSON parser chokes on.
- `aide explain`'s existing `--format human|agent|json` flag and
  behavior are unchanged — its local flag continues to shadow the new
  persistent one, and it keeps its third `agent` value that the new
  flag does not support.
- No change to any command's human-mode output. This is additive.
- An `AIDE_FORMAT` environment variable sets the default format when
  `--format` isn't passed explicitly, so a user or script can opt into
  JSON for a whole session (e.g. `export AIDE_FORMAT=json`) without
  repeating the flag on every invocation. Precedence: explicit
  `--format` flag > `AIDE_FORMAT` > `"human"` default. This still opt-in
  — nothing changes unless the user has deliberately set the variable.

## Non-goals

- Action/mutating commands (`aide sync`, `aide adopt`, `aide sandbox
  create`, `aide secrets rotate`, etc.) are out of scope for this
  design. They may get a JSON result envelope later; that is a
  separate design once this pattern is proven on read commands.
- Restyling human-mode output (colors, tables, borders via lipgloss /
  bubbles) is explicitly deferred to a separate follow-up. The struct
  retrofit this design requires is the natural seam for that later
  work (see Design), but no styling changes ship here.
- Adding a third `agent` format to commands other than `explain`. Only
  `human` and `json` are added globally; `agent` stays `explain`-only
  until a real need for it elsewhere shows up.
- Schema versioning/stability guarantees for the JSON output. Field
  names should be sensible and stable in practice, but no `--schema`
  flag or version field is introduced now.

## Design

### The `--format` flag

A new persistent flag on `rootCmd`, declared in `cmd/aide/main.go`
alongside the existing `--resolve` persistent flag. `AIDE_FORMAT`
supplies the flag's *default value* before parsing, so an explicit
`--format` on the command line always overrides it exactly like any
other cobra flag — no `Changed()` checks or custom precedence logic
needed:

```go
defaultFormat := "human"
if v, ok := os.LookupEnv("AIDE_FORMAT"); ok {
    defaultFormat = v
}
rootCmd.PersistentFlags().StringVar(&format, "format", defaultFormat,
    "Output format: human or json (default: $AIDE_FORMAT, or human)")
```

Every read/query command inherits it automatically via cobra's
persistent-flag inheritance — no per-command flag declaration needed.
An invalid `AIDE_FORMAT` value (anything other than `human`/`json`) is
caught by the same `output.Parse` validation every command already
runs against the resolved value, producing the same "unknown --format"
error an invalid flag would.

`explain` keeps its own local `cmd.Flags().StringVar(&format, "format", ...)`
exactly as today (`explain.go:80`), but its hardcoded `"human"` default
is replaced with the identical `AIDE_FORMAT`-aware resolution, so
`AIDE_FORMAT=json` also affects `explain`'s default — `"json"` is
already one of its three accepted values, so no change to its
validation switch is needed. Cobra resolves a local flag before an
inherited persistent flag of the same name on the same command, so an
explicit `explain --format agent` continues to override both the
default and the persistent flag, unmodified.

### `internal/output` package

New package, three responsibilities:

```go
package output

type Format string

const (
    Human Format = "human"
    JSON  Format = "json"
)

// Parse validates a raw --format value. Only "human" and "json" are
// accepted here; explain's "agent" value is handled entirely inside
// explain.go and never passes through this package.
func Parse(raw string) (Format, error)

// Emit writes data as indented JSON when format is JSON, otherwise
// calls humanFn to run the command's existing human-mode print logic.
func Emit(w io.Writer, format Format, data any, humanFn func(io.Writer) error) error

// PrintError writes err as a {"error": "..."} JSON object when format
// is JSON, otherwise writes err.Error() as plain text (matching
// cobra's current default error output). Used once, centrally, in
// main.go.
func PrintError(w io.Writer, format Format, err error)
```

`Emit`'s `data any` is whatever typed struct the calling command
builds; it must carry `json:` tags. `Emit` doesn't care what shape it
is — that keeps the package small and lets each command own its own
result type.

### Retrofitting a command: `cap list` as the worked example

Today `capListCmd` (`cap.go:117-200`) computes and prints each table
row inside the loop with no struct. Retrofit shape:

```go
type capListEntry struct {
    Name        string `json:"name"`
    Status      string `json:"status"`
    Source      string `json:"source"`
    Description string `json:"description"`
}

// ...inside RunE, after building names/registry/enabledSet/etc (unchanged):
entries := make([]capListEntry, 0, len(names))
for _, name := range names {
    // same per-entry computation as today, unchanged
    entries = append(entries, capListEntry{name, status, source, desc})
}

return output.Emit(out, format, entries, func(w io.Writer) error {
    fmt.Fprintf(w, "%-20s %-12s %-12s %s\n", "NAME", "STATUS", "SOURCE", "DESCRIPTION")
    for _, e := range entries {
        fmt.Fprintf(w, "%-20s %-12s %-12s %s\n", e.Name, e.Status, e.Source, e.Description)
    }
    return nil
})
```

This is a pure refactor: the loop body that computes `status`/`source`/
`desc` is unchanged, only *where* the values land (a struct field vs.
directly into `Fprintf`) changes, and the human-mode `Fprintf` calls
move into the `humanFn` closure verbatim. Human-mode output is
byte-for-byte identical to today.

The same shape applies to the other 21 ad hoc commands (`agents list`,
`sandbox list`, `env list`, `secrets list`, `hook list`, `context
list`, `plugin list`, `mcp list`, `cap variants`, `cap consent list`,
`sandbox guards`, `sandbox types`, `secrets keys`, plus the
free-text-report commands: `cap show`, `cap check`, `cap audit`, `cap
suggest-for-path`, `sandbox show`, `config show`, `status`, `prompt`).
Free-text commands still get a struct — even a one- or two-field one —
so `--format json` is uniform across every read command; there's no
"some commands don't support json" carve-out.

`aide which` (`status.go:190`) is the cheapest retrofit: its existing
`ui.BannerData` struct just needs `json:` tags added; no loop
refactor, since it's already fully typed.

`aide statusline` is a one-line machine-consumed string today (used in
shell prompts) — it gets a trivial `{"line": "..."}` wrapper under
`--format json` for consistency, though in practice no one will pass
`--format json` to it.

### Centralized error handling

`rootCmd` gets `SilenceErrors: true`. Cobra's `ExecuteC` only checks
the *root* command's `SilenceErrors` field before printing an error
itself (verified against the vendored cobra source), so this one field
suppresses cobra's default `Error: ...` stderr print for every
subcommand, without needing to touch each subcommand's `SilenceErrors`.

`main.go`'s existing post-`Execute()` block becomes the single place
errors are formatted:

```go
if err := rootCmd.Execute(); err != nil {
    output.PrintError(os.Stderr, format, err)
    var ee interface{ ExitCode() int }
    if errors.As(err, &ee) {
        os.Exit(ee.ExitCode())
    }
    os.Exit(1)
}
```

`format` here is the same variable bound to the persistent flag; by
the time `Execute()` returns, flag parsing has already populated it
(falls back to its `"human"` default if parsing itself failed before
reaching the flag, which is an acceptable edge case — an unparseable
invocation gets a human-readable error either way).

No per-command error-handling changes are needed to get this: every
`RunE` that already returns `fmt.Errorf(...)` today automatically gets
JSON-enveloped when `--format json` is set, because the enveloping
happens once, centrally, after `Execute()` returns.

## Research Findings

- Cobra's `ExecuteC` gates its own `Error: ...` stderr print on `!cmd.SilenceErrors && !c.SilenceErrors`, where `c` is always the root command. Setting `SilenceErrors: true` only on `rootCmd` is sufficient to suppress cobra's default error printing for every subcommand — no per-command change needed.
- A cobra local flag (declared via `cmd.Flags()`) shadows an inherited persistent flag of the same name on that command; confirmed against `explain.go:80`'s existing local `--format` flag, which will continue to resolve to `explain`'s own three-way switch rather than the new persistent flag.
- 24 read/query leaf commands were enumerated across `cmd/aide/*.go`; only `which` (`ui.BannerData`) and `explain` (`explain.Document`) currently have a typed struct behind their output. The remaining 22 build output inline with no intermediate structure — this is the bulk of the implementation work.

## Testing

- `internal/output`: unit tests for `Parse` (valid/invalid values),
  `Emit` (JSON path marshals `data` correctly; human path calls
  `humanFn` and does not touch `data`), and `PrintError` (JSON path
  produces valid `{"error": "..."}`; human path matches today's plain
  `err.Error()` text).
- Per retrofitted command: one test asserting `--format json` output
  unmarshals into the expected struct shape with the right values for
  a known fixture (mirroring however each command's existing human-mode
  tests already set up fixtures).
- One end-to-end test (or extend an existing one) confirming a command
  error under `--format json` produces a JSON error object on stderr
  and a non-zero exit code, and that human-mode error output is
  byte-for-byte unchanged from before this change.
- No existing human-mode golden-output tests should need updating,
  since the retrofit is designed to be output-preserving.
- `AIDE_FORMAT` precedence: unset → human; set to `json` with no
  `--format` flag → json; set to `json` with `--format human` passed
  explicitly → human (flag wins); set to an invalid value → the same
  "unknown --format" error a bad `--format` flag would produce.
