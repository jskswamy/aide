## Unreleased

### ✨ New

- Allow adding a loopback gateway without a bearer token when `/v1/models` returns 200 with no `Authorization` header. Persist an empty store/key only for localhost; remote hosts still need a token. Launch, sync, and harness writers omit bearer wiring when no key is stored.
- Show the default harness list (`claude`, `pi`, `opencode`, `codex`) on `aide gateway add`: prompt on a TTY with all four selected, print the same list on non-TTY and `--token-stdin`, and omit `harnesses:` from `config.yaml` when the full set is kept. `--harnesses` still overrides.

