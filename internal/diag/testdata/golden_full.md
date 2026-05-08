# aide diagnose report

> ⚠️  Review this file before sharing. It may contain paths, hostnames, or argv values that you consider sensitive. Secret values are redacted (only env-var names and lengths are recorded), but please skim every section before posting.

## TL;DR

exit=1 runtime=250ms — fast-fail (<500ms)

## Environment

- aide: 1.8.1 (commit abcd123, built 2026-05-07)
- os: darwin/arm64
- shell: /bin/zsh
- locale: en_US.UTF-8

## Invocation

- cwd: `/Users/alice/proj`
- config: `/Users/alice/.config/aide/secrets`
- agent binary: `/usr/bin/sandbox-exec`
- argv: `sandbox-exec -f /tmp/p.sb claude`

## Secrets wiring

- env `PATH` (len=80)
- env `ANTHROPIC_API_KEY` (len=51)
- secret source: `/Users/alice/.config/aide/secrets/secrets.yaml`
- age key source: yubikey

## Sandbox

- variants: network-outbound, code-only
- guards: network, filesystem, toolchain

<details><summary>rendered .sb</summary>

```scheme
(version 1)
(deny default)
```

</details>

## Child output (last 46 bytes)

```
error: An unknown error occurred (Unexpected)
```

## Reproduction

```
cd /Users/alice/proj && aide --diagnose -- sandbox-exec -f /tmp/p.sb claude
```
