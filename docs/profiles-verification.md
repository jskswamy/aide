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
| Pi | [Pi](#pi) | Pending verification |
| Codex | [Codex](#codex) | Pending verification |

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

---

## Pi

Pi selects its state with `PI_CODING_AGENT_DIR` (default
`~/.pi/agent`). Logins and API keys saved with `/login` live in that
dir's `auth.json`, which Pi locks while it refreshes an OAuth token.
Per Pi's provider docs (https://pi.dev/docs/latest/providers), a
provider's credential is resolved in this order: `--api-key`, the
`auth.json` entry, the environment variable, then `models.json`. So a
key aide injects from `credentials:` loses to a stored entry for the
same provider; aide prints a note when that happens. Sessions can be
moved with `PI_CODING_AGENT_SESSION_DIR`, which aide drops from the
shell for profile launches.

You need two logins for one provider that Pi signs in to with OAuth
(account A and account B; for example two GitHub Copilot accounts, or
two ChatGPT accounts through the `openai-codex` provider), plus an
OpenAI API key for P5. This checklist does not cover signing in to an
Anthropic subscription through Pi; do not use one here. `<p>` below is
the provider id you chose (`github-copilot`, `openai-codex`, ...).

aide passes arguments after `--` to Pi, so `aide --profile verify-a
--auth login -- auth check --provider <p> --json` runs Pi's own
credential check inside the profile's sandbox without starting a
session.

Record the Pi version:

```sh
pi --version
```

```text
Pi version (macOS):           (Linux):
```

### P0. Switch the test profiles to Pi

Replace the two S3 entries with:

```yaml
  verify-a:
    agent: pi
    config_dir_name: verify-a      # contexts: users write  profile: verify-a
  verify-b:
    agent: pi
    config_dir_name: verify-b      # contexts: users write  profile: verify-b
```

Note the state of the default Pi dir so you can tell later whether a
profile touched it:

```sh
ls -la ~/.pi/agent 2>&1 | tee /tmp/pi-global-before.txt
shasum -a 256 ~/.pi/agent/auth.json 2>/dev/null | tee -a /tmp/pi-global-before.txt
```

Expected: `aide which --profile verify-a` shows
`PI_CODING_AGENT_DIR = <home>/.pi-verify-a`.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P1. Each profile gets its own config dir

```sh
aide which --profile verify-b
aide sandbox test --profile verify-a > /tmp/verify-a.sb
grep -c 'pi-verify-a' /tmp/verify-a.sb     # expect 1 or more
grep -c 'pi-verify-b' /tmp/verify-a.sb     # expect 0
grep -cE '/\.pi(/agent)?("|$)' /tmp/verify-a.sb   # expect 0: no ~/.pi or ~/.pi/agent grant
```

Expected: verify-b shows `<home>/.pi-verify-b`; the sandbox profile
for verify-a grants `~/.pi-verify-a` only.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P2. Separate native logins

```sh
aide --profile verify-a --auth login    # /login, provider <p>, account A; then exit Pi
aide --profile verify-b --auth login    # /login, provider <p>, account B; then exit Pi
```

Then:

```sh
ls -la ~/.pi-verify-a/auth.json ~/.pi-verify-b/auth.json       # expect both, mode -rw-------
jq -r 'keys[]' ~/.pi-verify-a/auth.json ~/.pi-verify-b/auth.json
for p in verify-a verify-b; do jq -r '."<p>".refresh' ~/.pi-$p/auth.json | shasum -a 256; done   # expect two different hashes
aide --profile verify-a --auth login -- auth check --provider <p> --json   # expect "status":"ready"
aide --profile verify-b --auth login -- auth check --provider <p> --json   # expect "status":"ready"
ls -la ~/.pi/agent; shasum -a 256 ~/.pi/agent/auth.json 2>/dev/null       # compare with /tmp/pi-global-before.txt
```

Expected: each profile's `auth.json` holds its own `<p>` entry; the
default `~/.pi/agent/auth.json` is unchanged (or still absent).
Relaunch each profile and confirm it is still signed in (a prompt
works without `/login`).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P3. Concurrent runs

Open two terminals in `~/aide-verify`:

```sh
aide --profile verify-a     # terminal 1
aide --profile verify-b     # terminal 2
```

Send a short prompt in each (`say hi`), a second one in each while
both stay open, then exit both and relaunch each once.

Expected: both keep working with their own login; neither asks to
sign in again.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P4. Token refresh and the auth.json lock

Force a refresh in verify-a by marking its token expired, then let
Pi's credential check refresh it (it refreshes expired OAuth
credentials by default):

```sh
for p in verify-a verify-b; do shasum -a 256 ~/.pi-$p/auth.json; done
cp -f ~/.pi-verify-a/auth.json /tmp/verify-a-auth.bak
jq '."<p>".expires = 0' /tmp/verify-a-auth.bak > ~/.pi-verify-a/auth.json
chmod 600 ~/.pi-verify-a/auth.json
aide --profile verify-a --auth login -- auth check --provider <p> --json   # expect "status":"ready"
jq '."<p>".expires' ~/.pi-verify-a/auth.json                               # expect a future timestamp (ms)
for p in verify-a verify-b; do shasum -a 256 ~/.pi-$p/auth.json; done      # only verify-a's changes
```

Then refresh verify-a from two processes at once, to exercise the
lock:

```sh
jq '."<p>".expires = 0' ~/.pi-verify-a/auth.json > /tmp/verify-a-auth.exp && \
  cp -f /tmp/verify-a-auth.exp ~/.pi-verify-a/auth.json && chmod 600 ~/.pi-verify-a/auth.json
for i in 1 2; do aide --profile verify-a --auth login -- auth check --provider <p> --json & done; wait
jq empty ~/.pi-verify-a/auth.json && echo valid-json
ls -la ~/.pi-verify-a | grep -c 'auth.json.lock'                           # expect 0 once both exit
```

If your entry has no `expires` field, record its fields
(`jq '."<p>" | keys'`) instead and skip the forced refresh.

Expected:

- The refresh succeeds without `/login`, and only verify-a's
  `auth.json` changes; verify-b's and `~/.pi/agent/auth.json` do not.
- Both concurrent checks report `ready`; `auth.json` is still valid
  JSON and no `auth.json.lock` is left behind.
- No sandbox denial for the lock (on macOS run once with
  `aide --profile verify-a --diagnose-trace` if a check fails).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P5. API key versus stored login precedence

Store an OpenAI API key and bind it to verify-a (see
`docs/secrets.md`; reuse the C5 store if you have it):

```sh
aide secrets create verify --age-key <your age1... recipient>   # skip if it exists
aide secrets edit verify        # add: openai_api_key: <key>
```

```yaml
  verify-a:
    agent: pi
    config_dir_name: verify-a
    credentials:
      openai: {backend: age, store: verify, key: openai_api_key}
    default_credentials: [openai]
```

Now save a fake OpenAI key as a stored entry in verify-a: run
`aide --profile verify-a --auth login`, then `/login`, choose OpenAI
and enter `sk-not-real-stored-a` as the API key, then quit.

Run each case. `print-api-key` prints the key Pi would use; the
`grep -c` keeps your real key off the screen (1 means the stored fake
key won, 0 means the bound key won):

| Case | Command | Expected aide note | Expected `grep -c` |
| --- | --- | --- | --- |
| a | `aide --profile verify-a -- auth print-api-key --provider openai \| grep -c not-real-stored` | `<home>/.pi-verify-a/auth.json has a stored Pi login for openai: Pi uses it ahead of the selected API key ...` | 1 |
| b | `aide --profile verify-a --auth login -- auth print-api-key --provider openai \| grep -c not-real-stored` | `native login: not injecting credentials openai`, no stored-login note | 1 |
| c | in `aide --profile verify-a --auth login`, run `/logout` for OpenAI; then case a again | no stored-login note | 0 |
| d | `OPENAI_API_KEY=sk-not-real-shell aide --profile verify-b --auth login -- auth print-api-key --provider openai` | none | Pi reports no OpenAI credential (the shell key was scrubbed) |

Expected also: the aide note names only the provider; it never shows
`sk-not-real-stored-a` or your real key.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P6. Vertex and gcloud application-default credentials

Optional: needs a Google Cloud project with Vertex AI and
`gcloud auth application-default login` done once. Find Pi's Vertex
provider id with `pi --list-models vertex`.

```yaml
  verify-a:
    agent: pi
    config_dir_name: verify-a
    capabilities: [gcp]
    env:
      GOOGLE_CLOUD_LOCATION: us-central1
```

Add `GOOGLE_CLOUD_PROJECT: <project>` under that `env:` (aide warns
that it is a scrubbed cloud name set in `env:`, which is expected),
and give verify-b the same `capabilities: [gcp]` and `env:` entries.

```sh
aide --profile verify-a -- auth check --provider <vertex id> --json
aide --profile verify-b -- auth check --provider <vertex id> --json
```

Then remove `capabilities: [gcp]` from verify-b and repeat its check.

Expected (documented limit): with `gcp`, both profiles use the same
`~/.config/gcloud` login; aide does not isolate it. Without `gcp`,
verify-b cannot read it (not ready, or a permission error). Restore
the plain entries from P0 afterwards.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P7. Sandbox keeps other profiles out of reach

Pi has no shell escape of its own that runs before a model replies,
so this step swaps in a small script named `pi` that probes the
sandbox the way Pi would run. First make sure the files it probes
exist, so a denial means something:

```sh
for d in ~/.pi-verify-b ~/.pi/agent; do
  mkdir -p "$d"; [ -f "$d/auth.json" ] || { echo '{}' > "$d/auth.json"; chmod 600 "$d/auth.json"; }
done
mkdir -p ~/aide-verify/bin
cat > ~/aide-verify/bin/pi <<'SH'
#!/bin/sh
echo "PI_CODING_AGENT_DIR=$PI_CODING_AGENT_DIR"
echo "PI_CODING_AGENT_SESSION_DIR=${PI_CODING_AGENT_SESSION_DIR:-unset}"
for f in "$PI_CODING_AGENT_DIR/auth.json" "$HOME/.pi-verify-b/auth.json" "$HOME/.pi/agent/auth.json"; do
  if cat "$f" >/dev/null 2>&1; then echo "READ OK: $f"; else echo "READ DENIED: $f"; fi
done
touch "$PI_CODING_AGENT_DIR/probe" && echo "WRITE OK: own dir" && rm -f "$PI_CODING_AGENT_DIR/probe"
SH
chmod +x ~/aide-verify/bin/pi
```

Add to the config, at the top level (next to `profiles:` or
`contexts:`; merge into an existing `agents:` mapping if you have one).
The path must be absolute (`~` is not expanded there); print it with
`echo "$HOME/aide-verify/bin/pi"`:

```yaml
agents:
  pi:
    binary: /Users/<you>/aide-verify/bin/pi    # Linux: /home/<you>/...
```

```sh
cd ~/aide-verify
PI_CODING_AGENT_SESSION_DIR=/tmp/shared-pi-sessions aide --profile verify-a
```

Expected:

```text
PI_CODING_AGENT_DIR=<home>/.pi-verify-a
PI_CODING_AGENT_SESSION_DIR=unset
READ OK: <home>/.pi-verify-a/auth.json
READ DENIED: <home>/.pi-verify-b/auth.json
READ DENIED: <home>/.pi/agent/auth.json
WRITE OK: own dir
```

Remove the `agents: pi` entry afterwards, and any `{}` `auth.json`
the first command created.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P8. Shared paths

These are not login stores, but profiles share them:

- Project-local `.pi/settings.json` and `.pi/mcp.json` in a repository
  are read by every profile once trusted. The trust decision itself is
  recorded in each profile's own `trust.json`, so each profile asks
  once.
- Installed packages under `PI_PACKAGE_DIR` (Nix and Guix installs)
  are shared and read-only.

Check the trust prompt in a scratch repo:

```sh
mkdir -p ~/aide-verify/repo/.pi && cd ~/aide-verify/repo && git init -q
echo '{}' > .pi/settings.json
aide --profile verify-a      # expect a trust prompt; accept, then exit Pi
aide --profile verify-b      # expect a trust prompt again
ls ~/.pi-verify-a/trust.json ~/.pi-verify-b/trust.json
```

Expected: each profile asks once and records the decision in its own
dir; `~/.pi/agent/trust.json` is unchanged.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### P9. Pi cleanup

```sh
aide --profile verify-a --auth login    # /logout for each provider, then exit Pi
aide --profile verify-b --auth login    # /logout, then exit Pi
rm -rf ~/.pi-verify-a ~/.pi-verify-b ~/aide-verify/bin ~/aide-verify/repo
```

Remove the `openai_api_key` entry from the verify store (or the store
itself) if you created it for P5. Then run the shared cleanup (S4).

### Pi summary

```text
Ran by:                 Date:
macOS: all steps PASS? yes / no     Linux: all steps PASS? yes / no
Findings to carry into the spec:
```

---

## Codex

Codex selects its state with `CODEX_HOME` (default `~/.codex`). Per
OpenAI's docs (https://learn.chatgpt.com/docs/auth),
`cli_auth_credentials_store` in `config.toml` picks where a login is
kept: `file` (`$CODEX_HOME/auth.json`), `keyring` (the OS credential
store; fails if none is available) or `auto` (keyring when available,
else `auth.json`). Codex keys its keyring entry to a hash of the
canonical `CODEX_HOME`, so two homes should get two entries; this
section checks that. ChatGPT logins refresh automatically.
`CODEX_API_KEY`, `CODEX_ACCESS_TOKEN`, `OPENAI_API_KEY` and
`OPENAI_BASE_URL` exported in the shell are scrubbed, and so is any
variable a custom provider names in `env_key` in any Codex
`config.toml` aide knows of (`~/.codex`, every profile's Codex dir). Personal skills in `~/.agents/skills` are shared by every
Codex profile; they are not login state.

You need two ChatGPT accounts that can sign in to Codex (account A and
account B), and an OpenAI API key for X7. On a machine without a
browser, add `--device-auth` to `login`.

aide passes arguments after `--` to Codex, so
`aide --profile verify-a --auth login -- login status` runs Codex's
own check inside the profile's sandbox.

Record the Codex version:

```sh
codex --version
```

```text
Codex version (macOS):           (Linux):
```

### X0. Switch the test profiles to Codex

Replace the two S3 entries with:

```yaml
  verify-a:
    agent: codex
    config_dir_name: verify-a      # contexts: users write  profile: verify-a
  verify-b:
    agent: codex
    config_dir_name: verify-b      # contexts: users write  profile: verify-b
```

Note the global state:

```sh
ls -la ~/.codex 2>&1 | tee /tmp/codex-global-before.txt
shasum -a 256 ~/.codex/auth.json 2>/dev/null | tee -a /tmp/codex-global-before.txt
security dump-keychain 2>/dev/null | grep -A4 '"svce".*Codex' | tee /tmp/codex-keychain-before.txt   # macOS
secret-tool search --all service 'Codex Auth' 2>&1 | tee /tmp/codex-keyring-before.txt              # Linux (libsecret)
mkdir -p ~/.codex-verify-a ~/.codex-verify-b
```

Expected: `aide which --profile verify-a` shows
`CODEX_HOME = <home>/.codex-verify-a`.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X1. Each profile gets its own home

```sh
aide which --profile verify-b
aide sandbox test --profile verify-a > /tmp/verify-a.sb
grep -c 'codex-verify-a' /tmp/verify-a.sb             # expect 1 or more
grep -c 'codex-verify-b' /tmp/verify-a.sb             # expect 0
grep -cE '/\.codex("|$)' /tmp/verify-a.sb             # expect 0: no ~/.codex grant
grep -c '\.agents' /tmp/verify-a.sb                   # expect 0
```

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X2. Separate logins, file storage

```sh
for p in verify-a verify-b; do echo 'cli_auth_credentials_store = "file"' > ~/.codex-$p/config.toml; done
aide --profile verify-a --auth login -- login     # sign in as account A
aide --profile verify-b --auth login -- login     # sign in as account B
aide --profile verify-a --auth login -- login status
aide --profile verify-b --auth login -- login status
ls -la ~/.codex-verify-a/auth.json ~/.codex-verify-b/auth.json     # expect both, mode -rw-------
for p in verify-a verify-b; do jq -r '.tokens.account_id' ~/.codex-$p/auth.json | shasum -a 256; done   # expect two different hashes
shasum -a 256 ~/.codex/auth.json 2>/dev/null      # compare with /tmp/codex-global-before.txt
```

Expected: each `login status` says it is logged in using ChatGPT;
each home holds its own `auth.json` for its own account; `~/.codex`
is unchanged. If `account_id` is missing from your `auth.json`, hash
`.tokens.refresh_token` instead and note it.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X3. Separate logins, keyring storage

```sh
for p in verify-a verify-b; do
  aide --profile $p --auth login -- logout
  echo 'cli_auth_credentials_store = "keyring"' > ~/.codex-$p/config.toml
done
aide --profile verify-a --auth login -- login     # account A
aide --profile verify-b --auth login -- login     # account B
aide --profile verify-a --auth login -- login status
aide --profile verify-b --auth login -- login status
ls ~/.codex-verify-a/auth.json ~/.codex-verify-b/auth.json 2>&1   # expect: no such file
security dump-keychain 2>/dev/null | grep -A4 '"svce".*Codex'      # macOS: compare with the X0 list
secret-tool search --all service 'Codex Auth' 2>&1 | grep -E 'label|attribute'   # Linux
```

Expected: two new keyring entries, with different account
attributes, one per home; the X0 entries are unchanged. If the login
fails inside the sandbox (for example, no access to the keychain or
the D-Bus secret service), record the exact error: that is a finding,
not a reason to stop.

```text
Keyring service / account names seen:
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X4. Auto storage

```sh
for p in verify-a verify-b; do
  aide --profile $p --auth login -- logout
  echo 'cli_auth_credentials_store = "auto"' > ~/.codex-$p/config.toml
done
aide --profile verify-a --auth login -- login     # account A
aide --profile verify-b --auth login -- login     # account B
ls ~/.codex-verify-a/auth.json ~/.codex-verify-b/auth.json 2>&1
```

Expected: both profiles use the same store (keyring on macOS; on
Linux the keyring if a secret service runs, else `auth.json`), each
with its own entry or file, and each `login status` reports its own
account. Record which store was used.

```text
Store used (macOS):            (Linux):
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X5. Concurrent runs

In two terminals in `~/aide-verify`, run `aide --profile verify-a`
and `aide --profile verify-b`. Run `/status` in each, send a short
prompt in each, a second one while both stay open, then exit both and
relaunch each once.

Expected: each `/status` shows its own account throughout; neither
asks to sign in again.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X6. Token refresh stays in its own home

Use file storage for this step (repeat X2's `config.toml` line and
logins if you are on keyring). Codex refreshes a ChatGPT token when it
is old; make verify-a's look old:

```sh
for p in verify-a verify-b; do shasum -a 256 ~/.codex-$p/auth.json; done
cp -f ~/.codex-verify-a/auth.json /tmp/verify-a-codex-auth.bak
jq '.last_refresh = "2020-01-01T00:00:00Z"' /tmp/verify-a-codex-auth.bak > ~/.codex-verify-a/auth.json
chmod 600 ~/.codex-verify-a/auth.json
aide --profile verify-a -- exec "say hi"
jq -r '.last_refresh' ~/.codex-verify-a/auth.json          # expect a current timestamp
for p in verify-a verify-b; do shasum -a 256 ~/.codex-$p/auth.json; done   # only verify-a's changes
```

Optionally run `aide --profile verify-a -- exec "say hi"` in two
terminals at the same moment after resetting `last_refresh`, and check
`jq empty ~/.codex-verify-a/auth.json` afterwards (still valid JSON,
still signed in).

Expected: the prompt succeeds without a new login; only verify-a's
`auth.json` changes; verify-b's and `~/.codex/auth.json` do not.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X7. API key versus login precedence

Store an OpenAI API key and bind it to verify-a as Codex's API key
(see `docs/secrets.md`; reuse the C5/P5 store if you have it):

```sh
aide secrets create verify --age-key <your age1... recipient>   # skip if it exists
aide secrets edit verify        # add: openai_api_key: <key>
```

```yaml
  verify-a:
    agent: codex
    config_dir_name: verify-a
    credentials:
      codex: {backend: age, store: verify, key: openai_api_key}
    default_credentials: [codex]
```

The built-in `codex` binding injects `CODEX_API_KEY`. For each case,
run `/status` in the session (or `aide ... -- exec "say hi"` and check
your OpenAI usage page), and check the key inside the session without
printing it by asking Codex to run `printenv CODEX_API_KEY | wc -c`:

| Case | Command | Expected auth | Expected `wc -c` |
| --- | --- | --- | --- |
| a | `aide --profile verify-a -- exec "say hi"` | API key (usage appears on the key's project) | more than 0 |
| b | `aide --profile verify-a --auth login` | account A's ChatGPT login | 0 |
| c | `aide --profile verify-a --credential=` | account A's ChatGPT login | 0 |
| d | `CODEX_API_KEY=sk-not-real OPENAI_API_KEY=sk-not-real aide --profile verify-b` | account B's ChatGPT login | 0 for both names |

Also note whether interactive Codex (not `exec`) in case a uses the
key or the login; Codex documents `CODEX_API_KEY` for `exec`.

```text
Interactive codex with CODEX_API_KEY uses: key / login
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X8. Custom provider keys are scrubbed

Add a custom provider to verify-a's `config.toml` (keep the storage
line):

```sh
cat >> ~/.codex-verify-a/config.toml <<'TOML'

[model_providers.corp]
name = "Corp gateway"
base_url = "https://llm.invalid/v1"
env_key = "CORP_LLM_KEY"
TOML
```

The X9 probe shows whether `CORP_LLM_KEY` reaches the agent. Then
break the file and check the note:

```sh
cp -f ~/.codex-verify-a/config.toml /tmp/verify-a-config.bak
echo 'broken = [' >> ~/.codex-verify-a/config.toml
aide --profile verify-a --auth login -- login status
cp -f /tmp/verify-a-config.bak ~/.codex-verify-a/config.toml
```

Expected: the launch prints `aide: note: <home>/.codex-verify-a/config.toml
is not valid TOML (line N): custom model provider keys it names are
not scrubbed from the shell`, shows no file content, and continues
(Codex itself may then refuse the file; record what it says).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X9. Sandbox keeps other homes out of reach

As in P7, swap in a probe script named `codex`, after making sure the
files it probes exist:

```sh
for d in ~/.codex-verify-a ~/.codex-verify-b ~/.codex ~/.agents/skills; do mkdir -p "$d"; done
for d in ~/.codex-verify-a ~/.codex-verify-b ~/.codex; do
  [ -f "$d/auth.json" ] || { echo '{}' > "$d/auth.json"; chmod 600 "$d/auth.json"; }
done
mkdir -p ~/aide-verify/bin
cat > ~/aide-verify/bin/codex <<'SH'
#!/bin/sh
echo "CODEX_HOME=$CODEX_HOME"
echo "CORP_LLM_KEY=${CORP_LLM_KEY:+set}"
for f in "$CODEX_HOME/auth.json" "$HOME/.codex-verify-b/auth.json" "$HOME/.codex/auth.json" "$HOME/.agents/skills"; do
  if ls "$f" >/dev/null 2>&1; then echo "READ OK: $f"; else echo "READ DENIED: $f"; fi
done
SH
chmod +x ~/aide-verify/bin/codex
```

Add at the top level of the config (absolute path; print it with
`echo "$HOME/aide-verify/bin/codex"`):

```yaml
agents:
  codex:
    binary: /Users/<you>/aide-verify/bin/codex    # Linux: /home/<you>/...
```

```sh
cd ~/aide-verify
CORP_LLM_KEY=not-real aide --profile verify-a --auth login
CORP_LLM_KEY=not-real aide --profile verify-b --auth login
```

Expected for verify-a: `CODEX_HOME=<home>/.codex-verify-a`, an empty
`CORP_LLM_KEY=` line, `READ OK` for its own `auth.json`, `READ DENIED`
for the other three. For verify-b: its own home and an empty
`CORP_LLM_KEY=` line too: a name any Codex home declares is scrubbed
from every launch. Remove the `agents: codex` entry afterwards, and
any `{}` `auth.json` the first commands created.

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X10. Shared ~/.agents/skills

```sh
mkdir -p ~/.agents/skills/aide-verify-skill
printf -- '---\nname: aide-verify-skill\ndescription: verification marker\n---\nSay MARKER.\n' \
  > ~/.agents/skills/aide-verify-skill/SKILL.md
```

In `aide --profile verify-a` and `aide --profile verify-b`, list
skills (`/skills`). Then add `sandbox: false` to verify-b and list
them again.

Expected: with the sandbox, neither profile lists
`aide-verify-skill` (the path is not granted). Without the sandbox,
verify-b lists it: personal skills are shared across profiles, which
is resource sharing, not a login leak. Remove `sandbox: false` and the
skill afterwards (`rm -rf ~/.agents/skills/aide-verify-skill`).

```text
Result (macOS): PASS / FAIL / N/A    Notes:
Result (Linux): PASS / FAIL / N/A    Notes:
```

### X11. Codex cleanup

```sh
aide --profile verify-a --auth login -- logout
aide --profile verify-b --auth login -- logout
rm -rf ~/.codex-verify-a ~/.codex-verify-b ~/aide-verify/bin
```

Check that the keyring entries from X3/X4 are gone (compare with X0)
and delete any left over. Remove the `openai_api_key` entry from the
verify store if you added it for X7. Then run the shared cleanup (S4).

### Codex summary

```text
Ran by:                 Date:
macOS: all steps PASS? yes / no     Linux: all steps PASS? yes / no
Findings to carry into the spec:
```
