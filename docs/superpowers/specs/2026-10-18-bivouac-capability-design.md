# A built-in bivouac capability

Status: implemented for the read-only commands and the herdr session
cleanup on macOS; Linux and the VM-changing commands are not covered.

## Intent

`bivouac list` and `bivouac status` fail inside an aide sandbox on macOS:

```text
Error: open /Users/<user>/.local/state/.bivouac-state.json.lock: operation not permitted
```

The baseline policy grants none of bivouac's state or config paths. A
hand-written grant for the state directory does not help either: the lock
file lives in `~/.local/state`, next to the `bivouac` directory.

## Measured (2026-10-18, clean config, real CLI)

Run under `aide sandbox test` plus extra rules, with `XDG_CONFIG_HOME`
pointed at an empty directory so no personal grants leak in:

| Extra rules | `list` | `status` |
|---|---|---|
| none | fails on the lock file | fails on the lock file |
| lock file only | fails on `state.json` | fails on `state.json` |
| lock + state dir | works | fails on `secrets.yaml` |
| lock + state dir + config dir (read) | works | works |

`~/.pkl/cache` was not needed. `~/.cache/bivouac` is already writable in
the baseline.

## herdr (found when `bivouac session merge` stopped)

bivouac runs the `herdr` CLI to add, enable and remove its saved SSH
machine and to close workspaces. After a merge advanced `main`, cleanup
stopped on `~/.local/state/herdr/client/endpoints.json`. Measured with
the capability policy, real `herdr`:

| Command | Needs |
|---|---|
| `herdr workspace list` (local) | nothing extra |
| `herdr machine list` | read of `~/.local/state/herdr/client` |
| `herdr machine enable <id>` | write to that dir (the catalog is replaced atomically) |
| `herdr machine status` | the client dir, plus ssh (`~/.ssh/config_external`) |
| `herdr --machine <label> workspace list` | the client dir, ssh, and `network-bind` of a unix socket |

The socket is bound under `/private/var/folders` (the per-user temp
dir); a bind under `/private/tmp`, `~/.ssh` or herdr's own dirs did not
help. `~/.config/herdr`, `ssh-metadata` and `~/.local/share/herdr` are
not needed.

## Design

A built-in capability `bivouac` in `internal/capability/builtin.go`:

- Readable: `~/.config/bivouac`.
- Writable: `~/.local/state/bivouac`, the literal
  `~/.local/state/.bivouac-state.json.lock`, and
  `~/.local/state/herdr/client`.
- Allow: `network-bind` of a unix socket under `/private/var/folders`.

It is auto-included when `~/.config/bivouac` exists, using the same
mechanism as `ccstatusline` and `skillshare` (`AutoInclude`), and honours
`--without bivouac`. `--with bivouac` works on any machine.

Default XDG locations only. A custom `XDG_CONFIG_HOME` or
`XDG_STATE_HOME` is not resolved; that needs a launch-time resolver like
Skillshare's and is not worth it until someone needs it.

## Security analysis

- The config dir is read-only. `base.pkl` and presets there are evaluated
  by pkl on the host, so an agent that could edit them could influence
  what runs outside the sandbox. `secrets.yaml` is sops-encrypted;
  reading the ciphertext does not reveal a secret, and the age key is
  not granted.
- The state dir is writable. `state.json` records each instance's IP.
  An agent could edit it and point a later `bivouac ssh` or `sync` at a
  different host. ssh is not granted by this capability (`--with ssh`),
  which is what makes that edit useful, so the exposure is limited to
  sessions that also enable ssh.
- The lock file is a literal, so no sibling in `~/.local/state` becomes
  writable. Verified: `~/.local/state/other-probe` stays denied.
- The herdr client dir holds the saved machine list (target host, session
  name). Editing it could point herdr at another host, which is again
  only useful with ssh enabled. Siblings such as `ssh-metadata` stay
  denied (verified).
- The socket bind covers unix sockets under the per-user temp dir only.
  It lets the agent listen locally; it grants no outbound reach.
  `/private/var/folders` is shared by all accounts on the machine, so
  this is wider than one user's `$TMPDIR`; the OS file permissions on the
  per-user subdirectories still apply. A launch-time `$TMPDIR` resolver
  would narrow it and is not built.
- `~/.pkl/cache` stays out. A poisoned pkl package cache would be
  evaluated on the host.
- Verified denied with the capability on: write to `~/.config/bivouac`,
  write to `~/.pkl/cache`, read of `~/.ssh/digitalocean`.

## Verification (macOS)

Done: `bivouac list` and `bivouac status` under the generated policy
match their unsandboxed output (status stops at the same sops
decryption step in both, since the run is non-interactive); the four
denials above; auto-include appears in `aide sandbox test` with the real
config; `--with bivouac` works with an empty config.

herdr, done: `machine list`, `machine enable` (write, state restored and
compared), `machine status` and `--machine <label> workspace list`
under `--with bivouac,ssh` all match unsandboxed output; without ssh the
remote call fails at the connection, as expected; a write elsewhere under
`~/.local/state/herdr` stays denied. The full `session merge` was not
re-run by me, as it would change a real session. The user re-ran their
failing `bivouac session merge` with this capability and it completed.

Not done:

- `up`, `down`, `provision`, `sync`, `session`, `ssh`: they create or
  change VMs and cost money. `provision` evaluates pkl, so expect
  `~/.pkl/cache` and possibly write access to `~/.config/bivouac`
  (`known_hosts`, `sizes_cache.json`). Measure with a real instance.
- The sops/age/YubiKey path and network access.
- Linux. State paths are plain directory grants, which Landlock handles;
  untested until a VM exists.

## Rollout

Release note under New. Users can delete their hand-written bivouac
grants after upgrading; that is their step.
