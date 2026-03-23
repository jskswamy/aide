# README Rewrite: "Stop Babysitting Your Agent"

## Problem

The current README leads with credential switching — a real but niche problem. It buries the universal pain point: **every coding agent user faces the choice between "approve every action" (exhausting) and "run autonomously" (terrifying)**.

People scanning the README dismiss it as another AI slop project because the depth of what aide offers isn't visible upfront.

## Design Principles

- **Before/after contrast** — every section shows the painful way vs the aide way, so readers immediately recognize their own experience
- **Progressive disclosure** — start with zero-config, layer in complexity for those who need it
- **Three pillars visible early** — sandbox, unified UX, reproducibility all appear in the first scroll
- **Not Claude Code specific** — aide wraps any agent binary, including Python scripts

## Three Pillars

1. **Sandbox everything** — OS-native guardrails for any agent. No per-action prompts.
2. **One UX for all agents** — `aide` is the only command. Agent, credentials, sandbox resolved automatically.
3. **Reproducible** — config + encrypted secrets in git. Clone and go on any machine.

## Section-by-Section Structure

### Section 1: Hook + tagline

```markdown
# aide

Stop babysitting your agent.

One command. Any agent. Sandboxed, reproducible, zero decision fatigue.
```

Short, scannable. Sets the tone immediately.

### Section 2: The problem (2-3 sentences)

Frame the decision fatigue problem. You planned the work, you know what the agent needs to do, but instead of letting it execute, you're stuck evaluating every file read, every shell command, every network call. That's not autonomy — that's babysitting with extra steps.

Don't name specific tools or flags here. Keep it universal.

### Section 3: Before/After — Three pillars

Three side-by-side comparison blocks. Each block:
- **Before**: The painful status quo (specific, recognizable)
- **After**: How aide solves it (concrete, not hand-wavy)

#### 3a: Sandbox

| Before | After |
|--------|-------|
| Skip all permissions and pray nothing touches your SSH keys. Or click "allow" 200 times per session. | aide applies 20 OS-native guards by default. SSH keys, cloud credentials, browser data, password stores — blocked. The agent runs free inside guardrails. No prompts, no prayer. |

#### 3b: Unified UX

| Before | After |
|--------|-------|
| Each agent has its own CLI, config format, env vars. Switching agents means rewiring your workflow. | `aide` resolves the right agent, credentials, and sandbox from your project directory. One command, every project. Swap agents without changing how you work. |

#### 3c: Reproducibility

| Before | After |
|--------|-------|
| API keys in `.env` files, shell profiles, wrapper scripts. One bad commit and they're leaked. New machine means hours of setup. | Secrets are sops-encrypted with age keys. Config and encrypted secrets live in git. `git clone` your config on a new machine and you're done. |

### Section 4: Quick start

**Installation first** — keep all current install options (curl, INSTALL_DIR override, VERSION pin, `go install`, build from source).

Then four commands to know:

```bash
aide                    # Resolve context and launch the agent (sandboxed)
aide setup              # Interactive first-time configuration
aide --agent claude     # Override agent selection
aide sandbox guards     # See what the sandbox protects
```

Then show a minimal config file (single context, ~5 lines). Then a multi-context config for the reader who needs it. Rotate agents in examples — show `claude` in one, `codex` in another — to reinforce the multi-agent story.

### Section 5: Sandbox deep dive

- Table of what's protected (SSH, cloud, infra, browsers, password managers) — reuse existing content
- How to customize: `aide sandbox guard docker` / `aide sandbox unguard browsers`
- Note that this is macOS Seatbelt under the hood, with Linux (Landlock) planned
- Mention `pkg/seatbelt` is reusable as a Go library

### Section 6: Secrets

- Lifecycle: create, edit
- Secrets decrypt in-process, never plaintext on disk

### Section 7: Reproducibility

Three scenarios:
- **Personal**: `cd ~/.config/aide && git init` — encrypted secrets safe to commit
- **Team**: `git clone` shared config, add your age key
- **CI/Docker**: Copy config, set `SOPS_AGE_KEY` env var

### Section 8: Supported agents

One line: Claude, Codex, Aider, Goose, Amp, Gemini — any binary on PATH.

### Section 9: Development + docs links

Brief dev setup, link to full docs.

## Tone Guidelines

- Direct, slightly opinionated. "Stop babysitting" energy throughout.
- Technical but not academic. Show don't explain.
- No AI hype language. No "revolutionary", "game-changing", "powered by AI".
- Assume the reader is a developer who's already using a coding agent and is frustrated.

## Delta from Current README

The current README already covers most content. This is a **reframe, not a rewrite**. Specifically:

**What moves:**
- "Scenarios" section → dissolved into before/after blocks in Section 3
- "How It Works" (6-step numbered list) → becomes a subsection after quick start, trimmed to focus on the sandbox flow
- Sandbox table → stays, moves into Section 5

**What gets rewritten:**
- Opening: tagline + one-liner replace "One command. Right agent. Right credentials. Every project."
- The framing paragraph: new "decision fatigue" problem statement replaces the current implicit framing
- Before/after blocks are new content (Section 3)

**What stays as-is (reformatted only):**
- Installation commands (all 5 options)
- Configuration examples (minimal + multi-context)
- Secrets lifecycle commands
- Sandbox guard/unguard commands
- Reproducibility section (personal/team/CI)
- Supported agents list
- Development section
- All 8 doc links
- License

**What gets dropped:**
- Nothing. All existing content is preserved, just reorganized.

## What's NOT changing

- The actual content about configuration, secrets lifecycle, and sandbox mechanics is solid. It gets reorganized and reframed, not rewritten from scratch.
- Doc links stay. Installation options stay.
- License section stays.

## Success Criteria

1. A Claude Code user scanning for 10 seconds understands: "this makes autonomous agents safe"
2. The before/after blocks create instant "that's me" recognition
3. Someone with zero config can install and benefit (sandbox) in under 2 minutes
4. The depth of the project (20 guards, encrypted secrets, multi-context, 6+ agents) is visible without requiring deep reading
5. Nobody mistakes this for AI slop — the specificity and technical depth signal real engineering
