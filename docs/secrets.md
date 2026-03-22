# Secrets

## How Secrets Work

aide stores API keys and other credentials as YAML files encrypted with [sops](https://github.com/getsops/sops) using [age](https://age-encryption.org/) encryption. Each secret file lives at `~/.config/aide/secrets/<name>.enc.yaml`.

When aide launches, it decrypts the required secret file in-process using the sops Go library. The sops CLI is not required at runtime.

Reference a secret from a context config using the `secret` field:

```yaml
secret: personal
```

aide appends `.enc.yaml` automatically and resolves the full path under the secrets directory.

## Age Key Discovery

aide tries the following sources in order and uses the first one it finds:

1. **YubiKey** via `age-plugin-yubikey` (hardware-bound key, requires the plugin binary on `$PATH`)
2. **`$SOPS_AGE_KEY`**: inline key material, useful for CI environments
3. **`$SOPS_AGE_KEY_FILE`**: path to a key file at a custom location
4. **`$XDG_CONFIG_HOME/sops/age/keys.txt`**: the sops default key file (typically `~/.config/sops/age/keys.txt`)

## Creating Secrets

```sh
aide secrets create personal --age-key age1abc...
```

aide opens `$EDITOR` with a YAML template. After you save and close the editor, aide encrypts the file and writes it to the secrets directory. The plaintext is held in a temp file during the editing session and removed immediately after re-encryption.

The `--age-key` flag sets the recipient for encryption. To encrypt for multiple recipients, create with one key and then add more via `aide secrets rotate personal --add-key age1...`.

## Editing Secrets

```sh
aide secrets edit personal
```

aide decrypts the secret to a tmpfs temp file, opens `$EDITOR`, and re-encrypts the result when you close the editor. After re-encryption, aide displays a diff of added and removed keys so you can confirm the changes.

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

Rotation re-encrypts the secret for the updated recipient set. The plaintext is decrypted in-process and re-encrypted immediately; it is not written to persistent disk at any point during rotation.

## Security Guarantees

- Temp files used during editing are removed immediately after re-encryption.
- Decrypted values live in process memory and are passed to subprocesses as environment variables. They die with the process.
- The runtime directory is cleaned up by signal handlers on normal and abnormal exit.
- Any stale runtime directories from previous crashed sessions are removed on the next launch.

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
docker build --build-arg SOPS_AGE_KEY="$(cat ~/.config/sops/age/keys.txt)" .
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
