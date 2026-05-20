## Unreleased

### 🔒 Security

- **Bump `github.com/go-git/go-git/v5` from 5.19.0 to 5.19.1.**
  Closes two upstream advisories surfaced by Dependabot against the
  go-git transitively used by `aide`'s git-aware code paths:

  - **CVE-2026-45571 / GHSA-crhj-59gh-8x96 (medium, CVSS 5.4, CWE-22).**
    Path validation in go-git's checkout logic had drifted from
    canonical Git, letting a crafted repository payload modify files
    outside the intended worktree — including the repository's `.git`
    directory (and submodule `.git` dirs, since submodule dotgit
    materialization escapes the worktree filesystem isolation that
    otherwise contains the main repo). 5.19.1 restores the upstream
    checks.
  - **CVE-2026-45570 / GHSA-m7cr-m3pv-hgrp (low, CWE-116).** The SSH
    transport wrapped repository paths in single quotes without
    escaping embedded single quotes, diverging from canonical Git's
    `sq_quote_buf`. A path containing `'` could break out of the
    quoted region in the remote exec command. The vulnerable behavior
    is on the SSH *server* side (servers that re-evaluate
    `$SSH_ORIGINAL_COMMAND` through a shell); canonical `git-shell`
    setups are not affected. 5.19.1 ports `sq_quote_buf` so go-git's
    wire output is byte-identical to canonical Git's.

  Exploitation requires interacting with attacker-controlled
  repositories or shell-evaluating SSH servers — same threat model
  as cloning a hostile remote — but the upgrade is mechanical and
  the patched release is API-compatible.

### 🐞 Bug Fixes

- **Deterministic order for minimal-format `mcp_servers`.** When
  `config.yaml` used the legacy list-form syntax
  (`mcp_servers: [git, context7]`) under a minimal/flat config,
  `normalizeMinimal` rebuilt the synthesised default context's
  `MCPServers` slice by iterating the parsed `MCPServerMap` — a Go
  map, so iteration order is randomized. The slice came out in a
  different order on every run, surfaced as a flaky
  `TestLoad_MinimalConfig` on CI (`expected mcp_servers [git, context7],
  got [context7 git]`). The slice is now sorted lexicographically
  before `normalizeMinimal` returns, so callers see a stable order
  regardless of map seed. The original YAML sequence order was already
  destroyed at parse time — `MCPServerMap.UnmarshalYAML`'s sequence
  branch stores names as keys in a map — so a sort-on-emit is the
  smallest deterministic fix; full YAML-order preservation would
  require a parallel `[]string` or AST-level round-trip and is left
  as a follow-up if it ever proves necessary.
