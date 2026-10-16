# Profiles verification checklist

aide admits a harness for profiles only after a person has run it
against real logins on macOS and Linux and found no authentication
crossover between two profiles (see "Harness support assessment and
admission criteria" in
`docs/superpowers/specs/2026-10-16-multi-harness-profiles-design.md`).
Automated tests cover what aide controls: the scrubbed environment,
the injected config dir and the sandbox rules. This checklist covers
what only a real harness can show: where it stores logins, how it
refreshes them, and which credential it picks.

Each harness has its own section. Run the shared setup once, then the
harness section on each platform. Record a result for every step,
then report the filled-in file back so the spec's admission table can
be updated.

| Harness | Section | Status |
| --- | --- | --- |
| Claude Code | [Claude Code](#claude-code) | Pending verification |
| Pi | Added by the Pi admission task | Not written yet |
| Codex | Added by the Codex admission task | Not written yet |

## How to record results

Every step ends with a result block like this:

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

Replace the choices with one word and note anything unexpected:
error text, file names, a different `/status` line. N/A is for steps
you cannot run (for example, no Bedrock account); say why in the
notes. A FAIL does not stop the run. Finish the other steps, since the
spec needs every finding.

Never paste secret values (API keys, tokens, keychain contents) into
this file. Hashes and file names are enough.

## Shared setup

### S1. Record the environment and check tools

The steps use `jq` and a SHA-256 tool. macOS ships `shasum`; on Linux
use `sha256sum` wherever a step says `shasum -a 256` if `shasum` is
missing. macOS steps also use `security` (built in).

```sh
aide --version
git -C <your aide checkout> rev-parse --short HEAD
uname -a
sw_vers            # macOS only
jq --version
shasum -a 256 /dev/null || sha256sum /dev/null
```

```text
aide version / commit:
macOS version:                     Linux distro and kernel:
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### S2. Back up your aide config

The steps add throwaway profiles to your real config
(`~/.config/aide/config.yaml`, or `$XDG_CONFIG_HOME/aide/config.yaml`
when that is set). Keep a copy so you can restore it at the end.

```sh
cp -f ~/.config/aide/config.yaml ~/.config/aide/config.yaml.before-verify \
  || echo "no config yet; S4 deletes the file instead of restoring it"
```

### S3. Add two test profiles

Check which schema your config uses:

```sh
grep -nE '^(profiles|contexts):' ~/.config/aide/config.yaml
```

- **`profiles:`** (or no config file yet): use the snippets in this
  checklist as written.
- **`contexts:`**: either convert first (recommended), or keep
  `contexts:` and translate the snippets:
  - Convert: run `aide profile migrate`, read the preview, then
    `aide profile migrate --write`. Check that the file now starts its
    entries with `profiles:` (`grep -n '^profiles:'` as above), that
    `config.yaml.bak` was written, and that `aide which` still shows
    the profile you normally use. S4 restores your original file.
  - Keep `contexts:`: wherever a snippet here says
    `config_dir_name: <name>`, write `profile: <name>` instead (the
    legacy spelling of the same field); every other field is the same.
    `config_dir_name:` under `contexts:` is rejected with
    `config_dir_name: belongs to profiles: entries`.

Add these two entries **inside your existing `profiles:` (or
`contexts:`) mapping**, indented like your other entries. Do not paste
a second `profiles:` key. If you have no config file yet, add a first
line `profiles:` above them.

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a      # contexts: users write  profile: verify-a
  verify-b:
    agent: claude
    config_dir_name: verify-b      # contexts: users write  profile: verify-b
```

Every later snippet that shows `verify-a:` replaces the verify-a entry
above; snippets that start with an indented field (such as `env:`) are
added inside the verify-a entry.

Create a scratch project directory for the runs:

```sh
mkdir -p ~/aide-verify && cd ~/aide-verify
```

Expected: `aide which --profile verify-a` loads without error and
shows `CLAUDE_CONFIG_DIR` pointing at `~/.claude-verify-a`.

```text
Schema used: profiles / contexts (kept) / contexts (migrated)
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### S4. Cleanup (after every harness section)

```sh
if [ -f ~/.config/aide/config.yaml.before-verify ]; then
  mv -f ~/.config/aide/config.yaml.before-verify ~/.config/aide/config.yaml
else
  rm -f ~/.config/aide/config.yaml   # there was none before S3
fi
# If S3's aide profile migrate --write created config.yaml.bak, delete
# it by hand once you have confirmed it is that copy.
rm -rf ~/aide-verify
```

Each harness section lists the login state it created; remove it only
once you have recorded the results.

---

## Claude Code

Claude Code selects its state with `CLAUDE_CONFIG_DIR`. Per Anthropic's
docs (https://code.claude.com/docs/en/authentication), it keeps
`.credentials.json` under that dir and keys the macOS Keychain entry to
it. A keyless Claude Console sign-in and `ant` CLI profiles are stored
in `~/.config/anthropic` instead, outside the dir. Env credentials
outrank the native login, in this order: cloud provider
(`CLAUDE_CODE_USE_*`), `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY`,
`apiKeyHelper`, `CLAUDE_CODE_OAUTH_TOKEN`, Anthropic profile or
federation credentials, then the `/login` subscription.

You need two Claude accounts that can sign in (account A and account
B; a Pro/Max/Team account each, or one claude.ai account and one
Console account). Some optional steps also need an Anthropic API key,
a Claude Console organization, or a Bedrock, Vertex or Foundry
account.

In-session checks use Claude's `/status` command and its `!` shell
prefix, which runs a shell command inside the sandboxed session.

Record the Claude Code version:

```sh
claude --version
```

```text
Claude Code version (macOS):           (Linux):
```

### C1. Each profile gets its own config dir

```sh
aide which --profile verify-a
aide which --profile verify-b
aide sandbox test --profile verify-a > /tmp/verify-a.sb
```

Expected:

- `which` shows `CLAUDE_CONFIG_DIR = <home>/.claude-verify-a` and
  `<home>/.claude-verify-b`.
- `/tmp/verify-a.sb` contains `<home>/.claude-verify-a` and does not
  contain `.claude-verify-b`. On macOS it also contains
  `(literal "<home>/.claude-verify-a.lock")` and contains neither
  `(prefix "<home>/.claude.json")` nor `(literal "<home>/.claude.lock")`.

```sh
grep -c 'claude-verify-b' /tmp/verify-a.sb           # expect 0
grep -c '\.claude\.json"' /tmp/verify-a.sb           # expect 0 (macOS)
grep -c 'claude-verify-a\.lock' /tmp/verify-a.sb     # expect 1 (macOS)
grep -c 'config/anthropic' /tmp/verify-a.sb          # expect 0
```

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C2. Separate native logins

Before signing in, note the state of the global Claude files so you
can tell later whether a profile touched them:

```sh
ls -la ~/.claude/.credentials.json ~/.claude.json 2>&1 | tee /tmp/claude-global-before.txt
shasum -a 256 ~/.claude.json ~/.claude/.credentials.json 2>/dev/null >> /tmp/claude-global-before.txt
# macOS: list the Claude Code keychain entries that exist now
security dump-keychain 2>/dev/null | grep '"svce"' | grep -i claude | tee /tmp/claude-keychain-before.txt
```

Sign in each profile with a different account:

```sh
aide --profile verify-a --auth login    # /login with account A, then /status, then /exit
aide --profile verify-b --auth login    # /login with account B, then /status, then /exit
```

Expected:

- `/status` in verify-a shows account A, and in verify-b account B.
- Linux: `~/.claude-verify-a/.credentials.json` and
  `~/.claude-verify-b/.credentials.json` exist with mode `-rw-------`;
  `~/.claude/.credentials.json` is unchanged.
- macOS: two new keychain entries appear, one per profile dir, both
  different from the default `Claude Code-credentials` entry; no
  `.credentials.json` in either profile dir (unless the Keychain was
  locked, which Claude reports).
- Both platforms: `~/.claude-verify-a/.claude.json` and
  `~/.claude-verify-b/.claude.json` exist; `~/.claude.json` is
  unchanged.

```sh
ls -la ~/.claude-verify-a ~/.claude-verify-b
shasum -a 256 ~/.claude.json ~/.claude/.credentials.json 2>/dev/null   # compare with /tmp/claude-global-before.txt
security dump-keychain 2>/dev/null | grep '"svce"' | grep -i claude    # macOS; compare with the before list
```

Relaunch both profiles (`aide --profile verify-a`, `aide --profile verify-b`)
and confirm each is still signed in as its own account without
running `/login` again.

```text
Keychain service names seen (macOS):
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C3. Concurrent runs

Open two terminals in `~/aide-verify`:

```sh
aide --profile verify-a     # terminal 1
aide --profile verify-b     # terminal 2
```

In each, run `/status`, then send a short prompt (`say hi`). Leave
both open, send a second prompt in each, then exit both and relaunch
each once more.

Expected: each session keeps its own account throughout; neither
session asks to sign in again; neither relaunch shows the other
account.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C4. Token refresh stays in its own entry or file

A login refresh must update only that profile's keychain entry or
file. Record fingerprints, force or wait for a refresh in verify-a,
then compare.

Linux:

```sh
for p in verify-a verify-b; do shasum -a 256 ~/.claude-$p/.credentials.json; done
# Force a refresh: back up, then mark verify-a's access token expired.
cp -f ~/.claude-verify-a/.credentials.json /tmp/verify-a-credentials.bak
jq '.claudeAiOauth.expiresAt = 0' /tmp/verify-a-credentials.bak > ~/.claude-verify-a/.credentials.json
chmod 600 ~/.claude-verify-a/.credentials.json
aide --profile verify-a      # send one prompt, then /exit
jq '.claudeAiOauth.expiresAt' ~/.claude-verify-a/.credentials.json   # expect a future timestamp (ms)
for p in verify-a verify-b; do shasum -a 256 ~/.claude-$p/.credentials.json; done
```

macOS (substitute the service names recorded in C2):

```sh
for s in "<verify-a service>" "<verify-b service>" "Claude Code-credentials"; do
  security find-generic-password -s "$s" -w 2>/dev/null | shasum -a 256
done
```

Then keep a verify-a session open past its access-token expiry (or
leave it overnight) and send a prompt, or run with
`aide --profile verify-a --diagnose-trace` to capture sandbox denials
during the refresh. Re-run the fingerprint loop.

Expected:

- The prompt after the forced or natural expiry succeeds without a
  new `/login`.
- Only verify-a's fingerprint changes; verify-b's and the default
  entry's do not.
- No sandbox denial for `~/.claude-verify-a.lock` or `~/.claude.json`
  (macOS `--diagnose-trace` report). On Linux, note any refresh
  failure: Landlock cannot grant the not-yet-existing
  `~/.claude-verify-a.lock` sibling, so a lock Claude takes there
  would be denied.
- Optional: while verify-a refreshes, verify-b in another terminal
  keeps working (concurrent refresh).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C5. API key versus login precedence

Needs an Anthropic API key. Store it in an age store and bind it to
verify-a (see `docs/secrets.md`):

```sh
aide secrets create verify --age-key <your age1... recipient>
aide secrets edit verify        # add: anthropic_api_key: <key>
```

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a
    credentials:
      anthropic: {backend: age, store: verify, key: anthropic_api_key}
    default_credentials: [anthropic]
```

Run each case, check `/status`, and check whether the key is present
inside the session without printing it:

```text
!printenv ANTHROPIC_API_KEY | wc -c
```

| Case | Command | Expected `/status` | Expected `wc -c` |
| --- | --- | --- | --- |
| a | `aide --profile verify-a` | API key in use (approve it when Claude asks), account A's login marked unused | more than 0 |
| b | `aide --profile verify-a --auth login` | account A's claude.ai login | 0 |
| c | `aide --profile verify-a --credential=` | account A's claude.ai login | 0 |
| d | `ANTHROPIC_API_KEY=sk-ant-not-real aide --profile verify-b` | account B's login | 0 |
| e | `CLAUDE_CODE_OAUTH_TOKEN=not-real ANTHROPIC_AUTH_TOKEN=not-real aide --profile verify-b` | account B's login | `!printenv CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_AUTH_TOKEN \| wc -c` is 0 |

Also confirm that case a did not store the key in verify-b: relaunch
verify-b and check `/status` still shows account B's login.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C6. Anthropic profiles in ~/.config/anthropic

Optional: needs a Claude Console organization that allows keyless
sign-in (Claude Code v2.1.242 or later).

```sh
ls -laR ~/.config/anthropic 2>&1 | tee /tmp/anthropic-before.txt
```

a. Without `ANTHROPIC_CONFIG_DIR`: in `aide --profile verify-a`, run
   `/login`, choose the Console account and "Sign in with your Console
   account". Record what happens. The sandbox does not grant
   `~/.config/anthropic`, so expect the sign-in to fail to save or to
   report a permission error; it must not succeed by writing there.

b. With a per-profile Anthropic config dir: add to verify-a

   ```yaml
       env:
         ANTHROPIC_CONFIG_DIR: ~/.claude-verify-a/anthropic
   ```

   and repeat the sign-in. Then:

   ```sh
   ls -laR ~/.claude-verify-a/anthropic      # expect configs/ and credentials/ with a profile file
   ls -laR ~/.config/anthropic | diff /tmp/anthropic-before.txt -   # expect no difference
   ```

   In `aide --profile verify-b`, `/status` shows no `Profile` row and
   still account B.

Undo: in verify-a run `/logout`, and remove the `env:` entry.

```text
Does Claude Code honor ANTHROPIC_CONFIG_DIR? yes / no
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C7. apiKeyHelper in user settings

aide cannot scrub `apiKeyHelper`; it prints a note instead.

```sh
cp -f ~/.claude-verify-a/settings.json /tmp/verify-a-settings.bak 2>/dev/null || true
jq '. + {"apiKeyHelper": "echo sk-ant-not-real-helper"}' /tmp/verify-a-settings.bak 2>/dev/null \
  > ~/.claude-verify-a/settings.json || echo '{"apiKeyHelper": "echo sk-ant-not-real-helper"}' > ~/.claude-verify-a/settings.json
aide --profile verify-a --auth login
```

Expected:

- Before Claude starts, stderr shows
  `aide: note: <home>/.claude-verify-a/settings.json sets apiKeyHelper: ...`
  and does not show `echo sk-ant-not-real-helper`.
- `/status` reports the helper as the credential in use (requests
  fail with the fake key); this is the documented limit.

Then check that the global user settings do not reach a profile:

```sh
cp -f /tmp/verify-a-settings.bak ~/.claude-verify-a/settings.json 2>/dev/null || rm -f ~/.claude-verify-a/settings.json
cp -f ~/.claude/settings.json /tmp/claude-settings.bak 2>/dev/null || true
jq '. + {"apiKeyHelper": "echo sk-ant-not-real-global"}' /tmp/claude-settings.bak 2>/dev/null \
  > ~/.claude/settings.json || echo '{"apiKeyHelper": "echo sk-ant-not-real-global"}' > ~/.claude/settings.json
aide --profile verify-b
```

Expected: no aide note, and `/status` shows account B's login.
Restore `~/.claude/settings.json` from `/tmp/claude-settings.bak`
(or delete it if it did not exist).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C8. Bedrock, Vertex and Foundry selectors in profile env:

Provider selectors are ordinary profile `env:` entries; shell copies
are scrubbed. Run the provider(s) you have; without an account, the
selection check (`/status`) still applies, and requests are expected
to fail.

Use one of these as the verify-a entry at a time (each replaces the
verify-a entry from S3).

Bedrock, with your AWS credentials (`~/.aws` and `AWS_PROFILE`,
re-admitted by the `aws` capability). aide prints a warning naming
`AWS_REGION`, because it is a scrubbed cloud name set in `env:`; that
is expected.

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a
    capabilities: [aws]
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
      AWS_REGION: us-east-1
```

Vertex, with gcloud application-default credentials (the `gcp`
capability grants `~/.config/gcloud`):

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a
    capabilities: [gcp]
    env:
      CLAUDE_CODE_USE_VERTEX: "1"
      ANTHROPIC_VERTEX_PROJECT_ID: <project>
      CLOUD_ML_REGION: us-east5
```

Foundry, signed in with `az login` (Microsoft Entra; the `azure`
capability grants `~/.azure` and re-admits an exported service
principal):

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a
    capabilities: [azure]
    env:
      CLAUDE_CODE_USE_FOUNDRY: "1"
      ANTHROPIC_FOUNDRY_RESOURCE: <resource>
```

Foundry with an API key instead: put the key in the C5 store
(`aide secrets edit verify`, add `foundry_api_key: <key>`) and use

```yaml
  verify-a:
    agent: claude
    config_dir_name: verify-a
    credentials:
      foundry:
        backend: age
        store: verify
        key: foundry_api_key
        env: [ANTHROPIC_FOUNDRY_API_KEY]
        harnesses: [claude]
    default_credentials: [foundry]
    env:
      CLAUDE_CODE_USE_FOUNDRY: "1"
      ANTHROPIC_FOUNDRY_RESOURCE: <resource>
```

Checks:

```sh
aide --profile verify-a                              # /status: the provider; a prompt works with a real account
CLAUDE_CODE_USE_BEDROCK=1 aide --profile verify-b    # /status: account B's login, not Bedrock
```

Expected: verify-a uses the selected provider; verify-b keeps its
login, and `!printenv CLAUDE_CODE_USE_BEDROCK | wc -c` there is 0.
Restore the plain verify-a entry from S3 afterwards.

```text
Providers tried:
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C9. Sandbox keeps other profiles out of reach

Inside `aide --profile verify-a`, run each command with Claude's `!`
prefix:

```text
!ls ~/.claude-verify-b
!cat ~/.claude-verify-b/.claude.json
!cat ~/.claude.json
!ls ~/.claude
!ls ~/.config/anthropic
!ls ~/.claude-verify-a
```

Expected: the first five fail with "Operation not permitted" (macOS)
or "Permission denied" (Linux); the last lists verify-a's own files.
(A "No such file" is also a pass for paths that do not exist.)

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C10. Config dirs that differ only by case are refused (macOS)

Add a third profile whose dir differs from verify-a's only by case:

```yaml
  verify-case:
    agent: claude
    config_dir_name: Verify-a
```

```sh
aide which --profile verify-case
```

Expected on macOS (case-insensitive volume): the command stops with
`profiles share a native config dir` naming verify-a and verify-case.
Repeat once with `~/.claude-verify-a` moved aside
(`mv ~/.claude-verify-a ~/.claude-verify-a.moved`), so neither dir
exists; it must still stop. Move it back afterwards and remove
verify-case. On Linux (case-sensitive) the two dirs are distinct and
the config loads; record that as PASS.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### C11. Claude cleanup

After recording results, sign out and remove the test state:

```sh
aide --profile verify-a --auth login    # /logout, then /exit
aide --profile verify-b --auth login    # /logout, then /exit
rm -rf ~/.claude-verify-a ~/.claude-verify-b ~/.claude-verify-a.lock ~/.claude-verify-b.lock
rm -f ~/.config/aide/secrets/verify.enc.yaml   # the C5 store, if you created one
```

Then run the shared cleanup (S4).

### Claude summary

```text
Ran by:                 Date:
macOS: all steps PASS? yes / no     Linux: all steps PASS? yes / no
Findings to carry into the spec:
```
