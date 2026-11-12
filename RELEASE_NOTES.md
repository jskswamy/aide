## v3.6.1 (2026-11-12)

### ✨ New

- Allow adding a loopback gateway without a bearer token when `/v1/models` returns 200 with no `Authorization` header. Persist an empty store/key only for localhost; remote hosts still need a token. Launch, sync, and harness writers omit bearer wiring when no key is stored.
- Show the default harness list (`claude`, `pi`, `omp`, `opencode`, `codex`) on `aide gateway add`: prompt on a TTY with all five selected, print the same list on non-TTY and `--token-stdin`, and omit `harnesses:` from `config.yaml` when the full set is kept. `--harnesses` still overrides.
- Add `omp` (Oh My Pi) harness support across launcher discovery, OS sandboxing (`~/.omp` default, isolated per-profile `PI_CODING_AGENT_DIR` and `PI_CONFIG_DIR`), `--yolo` (`--auto-approve`), built-in credentials and custom gateways (`models.yml`), MCP/plugin/hook provisioning, and `aide skillshare targets`.
- Add `aide usage` to run `ccusage` scoped to the current directory's resolved profile by default, with `--profile <name>` to pin a profile, `--all` to aggregate existing data across profiles, `--setup` (and `--force`) to generate labeled `pi.stores` in `~/.claude/ccusage.json`, and temporary config isolation so single-profile runs ignore additive `pi.stores`.

### 🐛 Bug Fixes

- Format Claude's `/model` picker description using the configured gateway name instead of the upstream `provider` field, render context windows >= 1M in SI decimal units (`1M`, `1.5M`), and compare full materialized model metadata (`displayName`, `description`, `contextWindow`, and `maxOutputTokens`) during `claude`, `pi`, `omp`, and `opencode` drift checks so upstream metadata updates rewrite stale harness files on sync or launch.
- Write a literal placeholder `apiKey` alongside `authHeader: false` in Pi's `models.json` for tokenless gateways (and accept non-empty literal placeholders during inspection) so Pi displays custom no-auth gateway models in `/model` and `--list-models`.

### 🔧 Internal

- Update Go toolchain to `go1.26.11` to address `GO-2026-6859` in `net/http`.
