# Secrets

## How Secrets Work

aide stores API keys and other credentials as YAML files encrypted with [sops](https://github.com/getsops/sops) using [age](https://age-encryption.org/) encryption. Each secret file lives at `~/.config/aide/secrets/<name>.enc.yaml`.

When aide launches, it decrypts the required secret file in-process using the sops Go library. aide does not require the sops CLI at runtime.

Reference a secret from a context config using the `secret` field:

```yaml
secret: personal
```

aide appends `.enc.yaml` automatically and resolves the full path under the secrets directory.

## Setting Up Age

Without an age identity aide cannot decrypt anything, so this is the first
step on a new machine. A YubiKey-bound identity works too — see [Age Key
Discovery](#age-key-discovery) — but a software identity is the simplest
path to a working setup.

**1. Install age.**

```sh
# macOS
brew install age

# Linux (Debian/Ubuntu)
sudo apt install age

# Nix
nix-env -iA nixpkgs.age
```

**2. Generate a key and append it to your sops keys file.**

`age-keygen` prints a fresh identity to stdout. Append it to the sops
keys file at the OS-canonical location aide expects:

```sh
# macOS
KEYS_FILE="$HOME/Library/Application Support/sops/age/keys.txt"

# Linux
KEYS_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/sops/age/keys.txt"

mkdir -p "$(dirname "$KEYS_FILE")"
age-keygen >> "$KEYS_FILE"
chmod 600 "$KEYS_FILE"
```

A keys file may hold any number of identities (software keys and YubiKey
plugin entries) — appending is safe.

**3. Read back your public key.** This is the recipient you encrypt to:

```sh
grep '^# public key:' "$KEYS_FILE" | tail -n1
# → # public key: age1abc...
```

Copy the `age1...` value — you will pass it to `aide secrets create`.

**4. Verify aide can find the key.**

```sh
aide status
```

The status output reports which age source aide picked up. You should
see the path to your `keys.txt`. If not, see [Finding Your Existing Age
Key](#finding-your-existing-age-key).

## Quick Start: Wiring an Anthropic API Key

The most common first secret is an Anthropic API key consumed by the
Claude agent via `ANTHROPIC_API_KEY`. End-to-end:

**1. Create an API key.** Sign in at <https://console.anthropic.com/>,
go to *API keys*, create a new key, and copy the `sk-ant-...` value.

**2. Create an encrypted secret.** Use the public key from your
`keys.txt` (see [Setting Up Age](#setting-up-age) step 3):

```sh
aide secrets create personal --age-key age1abc...
```

aide opens `$EDITOR` with a YAML template. Replace it with:

```yaml
anthropic_api_key: sk-ant-XXXXXXXX
```

Save and close. aide encrypts the file to
`~/.config/aide/secrets/personal.enc.yaml`.

**3. Wire it into a context.** Two steps from the CLI: bind the
directory to the `claude` agent + secret store, then attach an env
variable that pulls from the store.

```sh
aide use claude --secret personal
aide env set ANTHROPIC_API_KEY --secret-key anthropic_api_key --global
```

What each command does:

- `aide use claude --secret personal` — binds the current working
  directory to a context that runs `claude` and decrypts
  `personal.enc.yaml` at launch.
- `aide env set ANTHROPIC_API_KEY --secret-key anthropic_api_key --global`
  — adds an env variable on the active context whose value comes from
  the `anthropic_api_key` field inside the bound secret. `--global`
  writes the entry to user-level config (`~/.config/aide/config.yaml`)
  so it applies wherever this context matches; drop `--global` to keep
  the entry in the project-local config.

If you forget which fields exist in your secret, run `aide env set
ANTHROPIC_API_KEY --pick --global` and aide presents an interactive
picker.

**Advanced: edit the config file directly.** The two commands above
write into `~/.config/aide/config.yaml`. If you prefer hand-editing,
the resulting block looks like this:

```yaml
contexts:
  personal:
    agent: claude
    secret: personal
    match: ~/projects/*
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
```

The `{{ .secrets.<key> }}` Go-template syntax injects the decrypted
value into `ANTHROPIC_API_KEY` for the agent process only.

**4. Verify and launch.**

```sh
aide secrets keys personal     # confirm 'anthropic_api_key' is listed
aide                           # decrypts in-process and runs claude
```

The plaintext API key never lands on disk: aide decrypts in-memory and
exposes it only to the agent process's environment for the lifetime of
that launch.

> **Tip — multiple accounts.** To keep separate Claude Code states for
> personal and work API keys (separate auth, MCP servers, history),
> pair this Quick Start with a per-context `CLAUDE_CONFIG_DIR`. See
> [Multi-Account Setups](contexts.md#multi-account-setups) in the
> contexts doc for the worked example.

## Age Key Discovery

aide tries the following sources in order and uses the first one it finds:

1. **YubiKey** via `age-plugin-yubikey` (hardware-bound key, requires the plugin binary on `$PATH`)
2. **`$SOPS_AGE_KEY`**: inline key material, useful for CI environments
3. **`$SOPS_AGE_KEY_FILE`**: path to a key file at a custom location
4. **Default key file**, in OS-canonical location (matches sops):
   - macOS: `~/Library/Application Support/sops/age/keys.txt`
   - Linux: `$XDG_CONFIG_HOME/sops/age/keys.txt` (typically `~/.config/sops/age/keys.txt`)
   - On macOS, aide also falls back to `~/.config/sops/age/keys.txt` for users with cross-platform setups.

## Finding Your Existing Age Key

If you've used sops before and aren't sure where your key file lives:

```sh
# macOS (most common)
ls -l ~/Library/Application\ Support/sops/age/keys.txt

# Linux / cross-platform
ls -l "${XDG_CONFIG_HOME:-$HOME/.config}/sops/age/keys.txt"
```

If your key is somewhere else, point aide at it explicitly:

```sh
export SOPS_AGE_KEY_FILE=/path/to/keys.txt
aide secrets keys personal
```

To see which source aide picked up, run:

```sh
aide status
```

## Creating Secrets

```sh
aide secrets create personal --age-key age1abc...
```

aide opens `$EDITOR` with a YAML template. After you save and close the editor, aide encrypts the file and writes it to the secrets directory. aide holds the plaintext in a temp file during the editing session and removes it immediately after re-encryption.

The `--age-key` flag sets the recipient for encryption. To encrypt for multiple recipients, create with one key and then add more via `aide secrets rotate personal --add-key age1...`.

## Editing Secrets

```sh
aide secrets edit personal
```

aide decrypts the secret to a temporary file, opens `$EDITOR`, and re-encrypts the result when you close the editor. After re-encryption, aide displays a diff of added and removed keys so you can confirm the changes.

## Listing and Inspecting

List all secrets with their recipients and the contexts that reference each one:

```sh
aide secrets list
```

Show the key names in a secret without revealing values:

```sh
aide secrets keys personal
```

## Rotating Recipients

Add a recipient (for example, when onboarding a new team member or registering a new machine):

```sh
aide secrets rotate personal --add-key age1newkey...
```

Remove a recipient (for example, when revoking access):

```sh
aide secrets rotate personal --remove-key age1oldkey...
```

Rotation re-encrypts the secret for the updated recipient set. aide decrypts the plaintext in-process and re-encrypts immediately, never writing it to persistent disk.

## Security Guarantees

- aide removes temp files immediately after re-encryption.
- Decrypted values exist only in the process's memory and environment. They are not written to disk and do not persist after the process exits.
- Signal handlers clean up the runtime directory on normal and abnormal exit.
- aide removes stale runtime directories from previous crashed sessions on the next launch.

## CI and Docker

Set `SOPS_AGE_KEY` to the inline age key. No key file or YubiKey is required.

```dockerfile
FROM ubuntu:24.04

ARG SOPS_AGE_KEY
ENV SOPS_AGE_KEY=${SOPS_AGE_KEY}

COPY . /app
WORKDIR /app

RUN aide --agent claude -- -p "run tests"
```

Pass the key at build or runtime:

```sh
# Linux
docker build --build-arg SOPS_AGE_KEY="$(cat ~/.config/sops/age/keys.txt)" .
# macOS
docker build --build-arg SOPS_AGE_KEY="$(cat ~/Library/Application\ Support/sops/age/keys.txt)" .
# or at runtime
docker run -e SOPS_AGE_KEY="AGE-SECRET-KEY-..." myimage
```

In GitHub Actions or similar CI systems, store the key in a repository secret and expose it as an environment variable:

```yaml
- name: Run aide task
  env:
    SOPS_AGE_KEY: ${{ secrets.SOPS_AGE_KEY }}
  run: aide --agent claude -- -p "run tests"
```
