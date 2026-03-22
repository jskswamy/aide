# Deployment

aide stores all config and encrypted secrets under `~/.config/aide/`. Encrypted secrets use age/sops and are safe to commit to git. Only holders of the age private key can decrypt them.

## Git-Tracking Your Config

```sh
cd ~/.config/aide
git init
git add -A
git commit -m "Initial aide config"
```

Push to any remote. On a new machine, clone and you're running:

```sh
git clone git@github.com:you/aide-config.git ~/.config/aide
```

Keep your age private key out of the repo. Store it separately (password manager, encrypted backup).

## Team Shared Config

Each team member clones the shared config repo:

```sh
git clone git@github.com:your-org/aide-config.git ~/.config/aide
```

Add each member's age public key as a recipient:

```sh
aide secrets rotate work --add-key $(age-keygen -y ~/.config/aide/keys/alice.pub)
```

Commit and push the re-encrypted secrets. Every team member decrypts with their own private key. No plaintext is ever shared.

To remove a member, remove their key and rotate:

```sh
aide secrets rotate work --remove-key age1abc...
git add -A && git commit -m "Revoke alice's access"
```

## Docker / CI

Embed config in the image and inject the age key at runtime via environment variable. Do not bake the private key into the image.

**Dockerfile:**

```dockerfile
FROM your-base-image

COPY --chown=app:app /path/to/aide-config /home/app/.config/aide

RUN aide --version
```

**Run with the age key injected:**

```sh
docker run -e SOPS_AGE_KEY="$(cat ~/.config/sops/age/keys.txt)" your-image aide --agent claude -- -p "run tests"
```

**GitHub Actions:**

Store the age private key as a repository secret named `SOPS_AGE_KEY`.

```yaml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Restore aide config
        run: git clone git@github.com:your-org/aide-config.git ~/.config/aide

      - name: Run aide
        env:
          SOPS_AGE_KEY: ${{ secrets.SOPS_AGE_KEY }}
        run: aide --agent claude -- -p "run tests"
```

## Multiple Machines

Generate one age key per device:

```sh
age-keygen -o ~/.config/sops/age/keys.txt
```

Add each device's public key as a recipient:

```sh
aide secrets rotate work --add-key age1device2pubkey...
```

If a device is lost, revoke it by removing its key and rotating:

```sh
aide secrets rotate work --remove-key age1lostdevice...
git add -A && git commit -m "Revoke lost laptop key"
```

All remaining devices can still decrypt. The lost key can no longer access any secrets.
