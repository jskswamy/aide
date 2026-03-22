# README & Documentation Rewrite Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite README as a compelling pitch (under 200 lines) and create standalone docs for each feature area.

**Architecture:** README sells the tool to evaluators. Detailed reference lives in docs/. Each doc file covers one concern. No MCP claims (not implemented). Follow stop-slop rules: no throat-clearing, no hedge words, no em dashes, active voice, respect the reader's intelligence.

**Tech Stack:** Markdown

**Writing rules (from stop-slop):**
- No throat-clearing openers ("Let me explain", "It's worth noting")
- No hedge words ("simply", "just", "easily", "seamlessly")
- No em dashes
- No rhetorical questions
- Active voice only
- No vague declaratives ("This is powerful because")
- Respect reader intelligence: state facts, skip the sell
- Varied sentence rhythm (no mechanical list-paragraph-list pattern)

**What exists vs what's claimed:**
- MCP: schema exists, not implemented. Remove all MCP from docs.
- Gemini: not in KnownAgents. Remove from agent list.
- Everything else: implemented and tested.

---

### Task 1: Rewrite README.md

**Files:**
- Rewrite: `README.md`

Target: under 200 lines. Audience: someone evaluating whether to try aide.

- [ ] **Step 1: Write the new README**

Structure (each section is a few lines, not a page):

**1. Title + one-line pitch + 3-line demo**
```markdown
# aide

One command. Right agent. Right credentials. Every project.

```bash
cd ~/work/project && aide    # claude with Bedrock credentials, sandboxed
cd ~/oss/repo && aide        # codex with personal OpenAI key
cd ~/scratch && aide         # auto-detects agent on PATH, no config needed
```
```

**2. Scenarios** — 3 concrete pain points, 2-3 sentences each. No abstract bullets.
- Personal vs work credentials (the Bedrock story)
- Sandboxing untrusted agent actions
- Team sharing encrypted keys

**3. Quick Start** — install + first three commands. No prerequisites wall.

**4. How It Works** — 6-step numbered flow. One sentence each.

**5. Configuration** — minimal config (5 lines of YAML) + multi-context config example. Link to docs/configuration.md for full reference.

**6. Sandbox** — why (2 sentences about approval fatigue), default policy table, link to docs/sandbox.md.

**7. Secrets** — one paragraph, 3 commands (create, edit, rotate). Link to docs/secrets.md.

**8. Reproducibility** — 3 tiny code blocks: personal git-track, team sharing, Docker/CI.

**9. Development** — `nix develop`, make commands, test-linux.

**10. Links** — bullet list linking to all docs/*.md files.

**11. License**

Do NOT include:
- MCP anything (not implemented)
- Gemini in agent lists (not in KnownAgents)
- Preferences section (minor, goes in docs/configuration.md)
- Full CLI command listings (goes in docs/cli-reference.md)
- Shell completions (one line in CLI reference)
- Agent config dir env vars table (goes in docs/sandbox.md)
- Detailed sandbox customization (goes in docs/sandbox.md)

- [ ] **Step 2: Review against stop-slop rules**

Read the written README and check:
- No "seamlessly", "simply", "just", "easily", "effortlessly"
- No "Whether you're...", "Imagine...", "What if..."
- No em dashes
- No "This allows you to..." (passive sell)
- Every sentence states a fact or shows a command
- Under 200 lines

- [ ] **Step 3: Commit**

```
Rewrite README as pitch document
```

---

### Task 2: Create docs/getting-started.md

**Files:**
- Create: `docs/getting-started.md`

Covers: zero-config passthrough, `aide setup` wizard, `aide init`, creating your first context, binding directories.

- [ ] **Step 1: Write the doc**

Sections:
1. Zero Config (agent on PATH, no config file)
2. Multiple Agents on PATH (--agent flag)
3. First-Time Setup (`aide setup` wizard flow)
4. Binding Directories (`aide use`)
5. Creating Config Manually (`aide init`)
6. Your First Multi-Context Setup (step-by-step walkthrough)

- [ ] **Step 2: Commit**

```
Add getting-started guide
```

---

### Task 3: Create docs/contexts.md

**Files:**
- Create: `docs/contexts.md`

Covers: context resolution, match rules (path, remote, glob), specificity, default context, per-project override (.aide.yaml), all `aide context` and `aide use` commands.

- [ ] **Step 1: Write the doc**

Sections:
1. What is a Context (agent + credentials + sandbox = context)
2. Match Rules (path patterns, git remote patterns, globs)
3. Resolution Order (specificity rules, default_context fallback)
4. Per-Project Override (.aide.yaml)
5. Managing Contexts (all `aide context *` commands with examples)
6. The `aide use` Shortcut
7. Default Context (auto-set behavior, `aide context set-default`)
8. Inspecting Context (`aide which`, `aide which --resolve`)

- [ ] **Step 2: Commit**

```
Add contexts documentation
```

---

### Task 4: Create docs/environment.md

**Files:**
- Create: `docs/environment.md`

Covers: env vars on contexts, template syntax, --from-secret, literal vs template values, all `aide env` commands.

- [ ] **Step 1: Write the doc**

Sections:
1. Env Vars Live on Contexts (not agents)
2. Literal Values
3. Template Syntax (`{{ .secrets.key }}`, `{{ .project_root }}`, `{{ .runtime_dir }}`)
4. Setting Env Vars (`aide env set` with examples)
5. The `--from-secret` Flag (interactive picker, explicit key)
6. Listing and Removing Env Vars
7. Clean Env Mode (`--clean-env` flag, `clean_env` config)

- [ ] **Step 2: Commit**

```
Add environment variables documentation
```

---

### Task 5: Create docs/secrets.md

**Files:**
- Create: `docs/secrets.md`

Covers: age keys, sops, create/edit/rotate flows, key discovery, CI setup, security model.

- [ ] **Step 1: Write the doc**

Sections:
1. How Secrets Work (sops-encrypted YAML, age keys, decrypted in-process)
2. Age Key Discovery (YubiKey, env var, key file, default location)
3. Creating Secrets (`aide secrets create`)
4. Editing Secrets (`aide secrets edit`, key diff output)
5. Listing and Inspecting (`aide secrets list`, `aide secrets keys`)
6. Rotating Recipients (`aide secrets rotate`)
7. Security Guarantees (never plaintext on disk, tmpfs temp files, signal cleanup)
8. CI/Docker Setup (SOPS_AGE_KEY env var)

- [ ] **Step 2: Commit**

```
Add secrets documentation
```

---

### Task 6: Create docs/sandbox.md

**Files:**
- Create: `docs/sandbox.md`

Covers: why sandbox, default policy, customization, profiles, agent config dirs, platform details, all `aide sandbox` commands.

- [ ] **Step 1: Write the doc**

Sections:
1. Why Sandbox (approval fatigue problem, pre-defined boundary solution)
2. On by Default (default policy table)
3. Agent Config Directories (CLAUDE_CONFIG_DIR, CODEX_HOME, etc.)
4. Customizing Per-Context (inline policy, _extra suffixes)
5. Quick CLI Adjustments (allow, deny, ports, network, reset)
6. Named Profiles (create, edit, remove, reference in config)
7. Disabling Sandbox (`sandbox: false`)
8. Platform Details (macOS sandbox-exec, Linux Landlock, bwrap fallback)
9. Debugging (`aide sandbox show`, `aide sandbox test`)

- [ ] **Step 2: Commit**

```
Add sandbox documentation
```

---

### Task 7: Create docs/configuration.md

**Files:**
- Create: `docs/configuration.md`

Covers: full config format, minimal vs full, all fields, preferences, validation.

- [ ] **Step 1: Write the doc**

Sections:
1. Config Location (`~/.config/aide/config.yaml`, XDG)
2. Minimal Format (flat, single context)
3. Full Format (agents + contexts)
4. Agent Definitions
5. Context Fields (match, agent, secret, env, sandbox)
6. Preferences (show_info, info_style, info_detail)
7. Per-Project Override (.aide.yaml fields)
8. Validating Config (`aide validate`)
9. Viewing and Editing (`aide config show`, `aide config edit`)

- [ ] **Step 2: Commit**

```
Add configuration reference
```

---

### Task 8: Create docs/cli-reference.md

**Files:**
- Create: `docs/cli-reference.md`

Covers: every command, every flag, organized by command group.

- [ ] **Step 1: Write the doc**

One section per command group. Each command shows: usage, flags, examples, notes.

Groups:
1. Root Command (`aide`, `--agent`, `--resolve`, `--yolo`, `--clean-env`)
2. `aide which` / `aide validate`
3. `aide init` / `aide setup`
4. `aide use`
5. `aide context *`
6. `aide env *`
7. `aide secrets *`
8. `aide sandbox *`
9. `aide agents *`
10. `aide config *`
11. Shell Completions

- [ ] **Step 2: Commit**

```
Add CLI reference
```

---

### Task 9: Create docs/deployment.md

**Files:**
- Create: `docs/deployment.md`

Covers: git-tracking config, team sharing, Docker/CI patterns.

- [ ] **Step 1: Write the doc**

Sections:
1. Git-Tracking Your Config (personal setup)
2. Team Shared Config (clone + add age key)
3. Docker / CI (COPY config, SOPS_AGE_KEY env var)
4. Multiple Machines (age key per device, rotate recipients)

- [ ] **Step 2: Commit**

```
Add deployment guide
```

---

### Task 10: Update cross-references

**Files:**
- Modify: `README.md` (add doc links at bottom)
- Modify: each `docs/*.md` (add "See also" links between related docs)

- [ ] **Step 1: Add links section to README**

```markdown
## Documentation

- [Getting Started](docs/getting-started.md)
- [Contexts](docs/contexts.md)
- [Environment Variables](docs/environment.md)
- [Secrets](docs/secrets.md)
- [Sandbox](docs/sandbox.md)
- [Configuration Reference](docs/configuration.md)
- [CLI Reference](docs/cli-reference.md)
- [Deployment](docs/deployment.md)
```

- [ ] **Step 2: Add cross-links in each doc**

Each doc links to related docs where relevant (e.g., contexts.md links to environment.md for env vars, sandbox.md links to contexts.md for per-context policy).

- [ ] **Step 3: Commit**

```
Add cross-references between docs
```
