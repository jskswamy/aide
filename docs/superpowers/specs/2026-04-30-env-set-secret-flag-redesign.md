# `aide env set` Secret Flag Redesign

**Status:** Draft
**Date:** 2026-04-30

## Problem

`aide env set --from-secret <X>` is broken in two layered ways.

### 1. Parsing bug

`cmd/aide/env.go:177` declares:

```go
cmd.Flags().Lookup("from-secret").NoOptDefVal = " "
```

`NoOptDefVal` enables `--from-secret` to be passed bare (for the interactive
picker). But pflag's contract says: any flag with `NoOptDefVal` accepts a value
**only** via `--flag=VALUE`, never `--flag VALUE`. So:

```
aide env set ANTHROPIC_API_KEY --from-secret firmus
```

is parsed as `ANTHROPIC_API_KEY` and `firmus` both being positional args, with
`--from-secret` taking the default `" "`. The validator at `env.go:60` then
rejects it: *"cannot specify both a value argument and --from-secret"*. The
help text shows `--from-secret api_key` (space form), so the documented usage
cannot work.

### 2. Conceptual conflation

The codebase treats two distinct concepts as one word, "secret":

- **Secret store** — an encrypted file under `~/.aide/secrets/<name>.enc.yaml`.
- **Secret key** — a key inside that store's decrypted map.

`ctx.Secret` holds a *store* name. `--from-secret` accepts a *key* name. A user
typing `--from-secret firmus` reasonably reads it as "use the firmus store,"
but the code interprets `firmus` as a key inside whatever store the context
already points at. Even with the parsing fix, the flag's name pushes users
toward the wrong mental model.

A related side effect: when `ctx.Secret == ""`, `env set` silently
auto-selects/prompts for a store and persists it back onto the context
(`env.go:119-126`). Running `env set` can mutate which store a context is
bound to, with no explicit confirmation.

### 3. Why tests didn't catch it

`grep -rn "from-secret" --include="*_test.go"` returns nothing. The `env set`
command has no end-to-end parsing tests. A test on `RunE` alone would miss the
`NoOptDefVal` quirk anyway — the bug lives in the parser, not the handler.

## Goals

1. `aide env set <KEY> --secret-key <X>` works with both space and `=`
   separators.
2. The CLI surfaces the (store, key) pair explicitly. Users can name the store
   when they want, or rely on the context binding when they don't.
3. `env set` no longer mutates `ctx.Secret` as a side effect.
4. Regression coverage: cobra-level parsing tests for `env set` flag matrix.
5. `--from-secret` is removed outright. No deprecation alias. The project
   has no active user base, so a clean break is preferable to carrying
   compatibility debt.

## Non-goals

- Reworking secret storage, decryption, or context resolution.
- Touching `env list` / `env remove`.
- Changing how `{{ .secrets.X }}` templates are rendered at launch.

## Design

### Flag surface

```
aide env set <KEY> [VALUE] [flags]

Flags:
  --secret-key <K>     Key inside the secret store to reference.
                       Required when using a secret (unless --pick).
  --secret-store <S>   Store name. Defaults to the context's bound store.
                       Does NOT mutate the context binding.
  --pick               Interactively pick a key from the (resolved) store.
                       Mutually exclusive with --secret-key.
  --context <name>     Target context (requires --global).
  --global             Apply to user-level config instead of project.
```

`--from-secret` is gone entirely. Invoking it produces cobra's standard
"unknown flag" error.

`--secret-key` is a plain string flag — no `NoOptDefVal`. Bare
`--secret-key` (no value) is an error with a hint pointing at `--pick`.

### Resolution rules

For `aide env set FOO`:

1. If `VALUE` positional is given → literal value, ignore secret flags. If
   any secret flag is also set, error.
2. Else if `--secret-key` or `--pick` is given → secret reference path:
   - **Store** = `--secret-store` if set, else `ctx.Secret` if set, else
     error: *"no secret store bound to context X; pass --secret-store or run
     `aide context set-secret`"*.
   - **Key** = `--secret-key` if set, else interactive pick from store's
     decrypted keys.
   - Render `{{ .secrets.<key> }}` as the value. The store name is **not**
     baked into the template — the launcher resolves it via the context's
     binding at run time. (Today's behavior; preserved.)
3. Else error: *"must specify VALUE, --secret-key, or --pick"*.

### Removing the side effect

`env.go:119-126` is deleted. If the context has no bound store and the user
didn't pass `--secret-store`, the command errors with the hint above instead
of silently writing `ctx.Secret`.

A separate, explicit operation owns the binding:

```
aide context set-secret <store-name> [--context X] [--global]
```

(Already in scope of the `aide context` subcommand surface; if the subcommand
doesn't exist yet, this design adds it as a one-liner that writes
`ctx.Secret` and confirms.)

### Help text

The `Long:` block becomes:

```
Set an environment variable on a context.

Examples:
  aide env set ANTHROPIC_API_KEY sk-ant-xxx                 # literal value
  aide env set ANTHROPIC_API_KEY --secret-key api_key       # key in bound store
  aide env set ANTHROPIC_API_KEY --secret-store firmus --secret-key api_key
  aide env set ANTHROPIC_API_KEY --pick                     # interactive
  aide env set OPENAI_API_KEY --secret-key key --context work --global
```

Every example must be exercised by a test (see Testing).

## Testing

### New: cobra parsing harness

Add `cmd/aide/env_set_test.go`. Each test builds the `envSetCmd()`, calls
`cmd.SetArgs([...])`, redirects stdout/stderr via `cmd.SetOut`/`SetErr`, and
runs `cmd.Execute()`. The handler is stubbed where it would touch disk
(config writer, secrets decrypter) via small interfaces injected through the
existing `cmdEnv` indirection.

Cases:

| Args                                                                | Expected                          |
| ------------------------------------------------------------------- | --------------------------------- |
| `set FOO bar`                                                       | literal value `bar` written       |
| `set FOO --secret-key api_key` (ctx has store)                      | template `{{.secrets.api_key}}`   |
| `set FOO --secret-key=api_key`                                      | same                              |
| `set FOO --secret-key api_key --secret-store firmus`                | uses firmus, no ctx mutation      |
| `set FOO --pick`                                                    | invokes picker                    |
| `set FOO --secret-key api_key --pick`                               | error: mutually exclusive         |
| `set FOO bar --secret-key api_key`                                  | error: literal + secret           |
| `set FOO`                                                           | error: nothing specified          |
| `set FOO --secret-key`                                              | error: missing value, hint --pick |
| `set FOO --secret-key api_key` (ctx has no store, no --secret-store)| error: no store bound             |
| `set FOO --from-secret=api_key`                                     | error: unknown flag (cobra)       |
| `set FOO --from-secret api_key`                                     | error: unknown flag (cobra)       |

### Examples-as-tests guard

Add a test that parses the `Long:` field of every command in `cmd/aide`,
extracts lines matching `aide <subcommand> ...`, and asserts each parses
without error against the corresponding cobra command (handler stubbed).
This catches future regressions where help drifts from reality.

## Migration / compatibility

The project has no active user base, so this is a clean break with no
deprecation period. Concretely:

- `--from-secret` is removed. Anyone invoking it gets cobra's "unknown
  flag" error and must switch to `--secret-key` (and optionally
  `--secret-store` / `--pick`).
- The implicit `ctx.Secret` auto-bind is removed. Contexts without a
  bound store must be configured with `aide context set-secret` before
  `env set` can write secret references.
- README, design notes, and any other docs that reference `--from-secret`
  are updated in the same PR.

## Rollout

1. Add parsing test harness — write the new test matrix, all tests red.
2. Implement `--secret-key`, `--secret-store`, `--pick`; remove
   `--from-secret` and its `NoOptDefVal` line. Tests go green.
3. Delete the auto-bind side effect. Add `aide context set-secret` if
   the subcommand surface doesn't already provide it.
4. Update help text; the examples-as-tests guard ensures every example
   parses.
5. Sweep README and `docs/` for `--from-secret` references and rewrite
   them.

## Open questions

- Should `--pick` also apply to picking the *store* when none is bound? Or
  keep store selection strictly explicit? Current draft: explicit only. A
  separate `--pick-store` could be added later if friction warrants.
