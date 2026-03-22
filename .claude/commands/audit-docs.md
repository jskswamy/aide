# Documentation Audit

Audit all documentation for accuracy against source code and writing quality against stop-slop rules.

## Process

Run two passes on every doc file. Report all issues. Optionally fix them.

### Files to audit

- `README.md`
- `docs/getting-started.md`
- `docs/contexts.md`
- `docs/environment.md`
- `docs/secrets.md`
- `docs/sandbox.md`
- `docs/configuration.md`
- `docs/cli-reference.md`
- `docs/deployment.md`

### Source files to cross-reference

- `cmd/aide/commands.go` — all CLI commands, flags, and subcommands
- `cmd/aide/main.go` — root command flags
- `internal/config/schema.go` — config struct fields and YAML tags
- `internal/launcher/passthrough.go` — KnownAgents list
- `internal/launcher/agentcfg.go` — agent config dir env vars
- `internal/sandbox/sandbox.go` — default sandbox policy
- `internal/sandbox/policy.go` — PolicyFromConfig, sandbox config fields

## Pass 1: Accuracy Audit

For each doc file, check every factual claim against the source code.

**Commands and flags:**
- Every `aide <command>` mentioned exists in commands.go
- Every `--flag` mentioned exists with the correct type (bool, string, stringSlice)
- Flag descriptions match what the code does
- Positional argument counts match `cobra.ExactArgs` / `cobra.MaximumNArgs` etc.

**Config fields:**
- Every YAML key shown matches a struct field's yaml tag in schema.go
- Template variables (`{{ .secrets.* }}`, `{{ .project_root }}`, `{{ .runtime_dir }}`) match what template.go provides
- No references to unimplemented features (MCP servers, Gemini agent)

**Behavior claims:**
- "automatically does X" — verify the code path actually does X
- Default values match what the code sets
- Error messages shown match actual error strings
- File paths match actual XDG resolution

**Severity levels:**
- **WRONG** — factually incorrect (command doesn't exist, flag has wrong type, feature not implemented)
- **MISLEADING** — implies something not true (tmpfs when it's os.TempDir, "multiple" when it's single)
- **MISSING** — implemented feature not documented
- **STALE** — was true but code changed since doc was written

## Pass 2: Writing Quality (stop-slop)

Check every doc file against these rules.

**Banned words** (search literally):
- seamlessly, simply, just, easily, effortlessly, robust, powerful
- leverage, utilize, comprehensive, cutting-edge, innovative
- game-changing, world-class, best-in-class, next-generation
- harness, unlock, empower, elevate, streamline, optimize
- delve, dive into, take a deep dive, unpack

**Banned patterns:**
- Throat-clearing openers: "Let me explain", "It's worth noting", "In today's world", "As we know"
- Em dashes (the — character)
- Rhetorical questions: "What if you could...?", "Ever wondered...?", "Why not...?"
- False agency: "It could be argued", "One might say", "It's important to note"
- Dramatic fragmentation: "Short. Choppy. Sentences. For. Effect."
- Passive voice: "is used by", "was created", "can be configured" (prefer active: "uses", "creates", "configure")
- Vague declaratives: "This is powerful because", "This provides a robust", "This enables seamless"
- Over-explaining: repeating the same concept in different words
- Binary contrast setup: "Unlike X, aide does Y" (state what aide does, skip the comparison)

**Quality checks:**
- Active voice throughout
- Varied sentence rhythm (no mechanical short-long-short-long)
- Respects reader intelligence (no "as you can see", "note that", "keep in mind")
- Every sentence adds information (delete those that don't)
- Code examples match the surrounding prose

## Output Format

```
## Accuracy Issues

### [filename]
- Line N: WRONG — claims `aide run my-task` exists. No `run` subcommand in commands.go.
- Line N: MISLEADING — says "tmpfs-backed temp file". Code uses os.TempDir(), not guaranteed tmpfs.
- Line N: STALE — shows `--secrets` flag, renamed to `--secret` in commit 51afd84.

## Writing Quality Issues

### [filename]
- Line N: Banned word "seamlessly"
- Line N: Em dash found
- Line N: Passive voice: "is configured" → "configure"
- Line N: Throat-clearing: "It's worth noting that" → delete, state the fact directly

## Summary
- Accuracy: N issues (X wrong, Y misleading, Z stale)
- Writing: N issues
- Files clean: [list]
```

## Fix Mode

If the user says "fix" or "fix all", make the corrections directly in the doc files. For accuracy issues, fix only if the correct information is clear from the source code. For ambiguous cases, flag them and ask.

For writing quality issues, rewrite the offending line following the stop-slop rules. Preserve the technical content. Change only the wording.

After fixing, re-run both passes to verify zero issues remain.
