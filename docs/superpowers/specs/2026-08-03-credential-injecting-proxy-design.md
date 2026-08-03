# Credential-Injecting Proxy Design

**Status:** Approved  
**Date:** 2026-08-03  
**Inspired by:** [Tailscale blog — Hugging Face intrusion](https://tailscale.com/blog/hugging-face-intrusion) (credential-injecting proxy pattern)

---

## Problem

Aide currently injects credentials (API keys, service tokens) into the agent
process as environment variables. Any tool the agent spawns inherits those
variables. A compromised agent or child process can read, log, or exfiltrate
the real credentials — they live in the process environment for the full
session lifetime.

The credential-injecting proxy pattern solves this: the agent never holds real
credentials. It holds worthless dummy tokens. A proxy intercepts outbound
requests, swaps dummy tokens for real credentials, and forwards to the upstream
service. A compromised agent can exfiltrate its dummy token — but that token
has no value outside the proxy session.

---

## Design

### Core principle

The agent and all its child processes see only dummy tokens. Real credentials
live exclusively inside aide's proxy process. The proxy is the only path to the
internet (enforced at the OS network layer by AIDE-dpe.6).

```
┌──────────────────────────────────────────────────────────┐
│ aide process                                              │
│                                                           │
│  ┌─────────────┐   ┌──────────────────┐   ┌──────────┐  │
│  │   Agent     │──▶│ Cred Inspector   │──▶│  Filter  │──┼──▶ internet
│  │ (sandboxed) │   │ (TLS termination)│   │ (Mode A) │  │
│  └─────────────┘   └──────────────────┘   └──────────┘  │
│        │                    │                             │
│  dummy tokens          permanent                         │
│  in env                aide CA                           │
└──────────────────────────────────────────────────────────┘
```

### Architecture chain

```
agent → Credential Inspector → Filter (Mode A) → internet
```

The Credential Inspector is a new `Inspector` implementation under the
Mode B interface (`AIDE-hze.1`). It composes with — but does not duplicate —
the CA infrastructure from `AIDE-hze.4`.

---

## TLS Termination

The Inspector terminates TLS for all outbound HTTPS and gRPC traffic. This is
the only way to read and modify the `Authorization` header inside an encrypted
session.

**Per-connection flow:**

1. Agent SDK sends `CONNECT api.anthropic.com:443` to the proxy Unix socket
2. Inspector generates an in-memory leaf cert for `api.anthropic.com`, signed
   by the permanent aide CA
3. Agent completes TLS handshake with the Inspector (trusts aide CA via
   `SSL_CERT_FILE` / `NODE_EXTRA_CA_CERTS` etc.)
4. Inspector reads the plaintext HTTP request, sees `Authorization: aide-tok-xxxx`
5. Inspector swaps dummy token → real credential from the token registry
6. Inspector opens a new TLS connection to `api.anthropic.com` (real cert,
   real server), forwards the modified request
7. Response flows back; agent sees a normal HTTP response

**gRPC:** identical flow. HTTP/2 HPACK headers decompress to plain text;
`Authorization` is visible and swappable after TLS termination. gRPC-Go and
gRPC-Node both work transparently. gRPC-Python has an open ALPN bug (#32172)
with TLS-terminating proxies — documented as a known limitation for v1.

---

## Permanent Aide CA

One CA per aide installation, not per session.

- Generated at `aide init` (or first run if not present)
- Private key stored encrypted in aide's config directory
  (`~/.config/aide/ca-key.pem`, protected by aide's existing secrets
  infrastructure)
- CA certificate installed into the platform trust store once, via logic
  vendored from mkcert (`internal/truststore`):
  - **macOS**: `security add-trusted-cert` into user keychain
  - **Linux**: NSS database + `update-ca-certificates` / `update-ca-trust`
  - **Windows**: Windows certificate store
- User sees one trust prompt ever; subsequent sessions are silent
- Rotation: `aide ca rotate` — generates new keypair, replaces keychain entry

**Why permanent (not per-session):**  
Once the private key is gone, the CA cert in the keychain is harmless — it
cannot sign new leaf certs. Per-session CAs create keychain clutter and a
trust prompt on every `aide` invocation. The actual security property is key
security, not CA rotation frequency.

**mkcert trust store vendoring:**  
mkcert (`filippo.io/mkcert`) is `package main` — not importable. Its
platform-specific trust store files (`truststore_darwin.go`,
`truststore_linux.go`, `truststore_nss.go`, `truststore_windows.go`) are
MIT-licensed and self-contained. They are vendored into
`internal/truststore` with `package main` → `package truststore` and
relevant functions exported.

---

## Credential Sources

Resolved at session start (token-issuance time), not at swap time.

| Provider | Source | Refresh |
|---|---|---|
| Anthropic | aide secrets (SOPS) | static |
| GCP | `gcloud auth print-access-token` | 1 hr TTL, re-issued before expiry |
| AWS | `aws sts get-session-token` | configurable TTL |
| Custom | aide secrets, domain → secret name mapping | static |

Credential helpers run in aide's process space with aide's clean `PATH` —
never the agent's environment. Helper binary paths are validated by absolute
path at startup.

---

## Token Registry

In-memory map inside the aide process (not the proxy subprocess).

```
DummyToken → {
    RealCredential  string
    TargetDomain    string    // e.g. "api.anthropic.com"
    PID             int       // agent PID
    StartTime       uint64    // process start time (PID+start-time pair prevents PID-reuse)
    ExpiresAt       time.Time // matches underlying credential TTL
}
```

Tokens are:
- Cryptographically random, 128-bit minimum
- Scoped to one domain (1:1 mapping; proxy ignores all request content when
  selecting credential — mapping is immutable for the token lifetime)
- Invalidated synchronously on session teardown

---

## Proxy Authentication

The Inspector binds to a **Unix domain socket** (not TCP localhost).
`SO_PEERCRED` on each incoming connection verifies the caller PID is in the
agent's process group. This prevents any other process on the machine from
using the proxy as a credential vending machine.

---

## Agent Environment

What the agent and its children receive:

```
HTTPS_PROXY=unix:///path/to/session/proxy.sock
https_proxy=unix:///path/to/session/proxy.sock   # lowercase for gRPC-Node
ANTHROPIC_API_KEY=aide-tok-xxxx                  # dummy
SSL_CERT_FILE=/path/to/aide-ca.pem               # Linux Go tools
NODE_EXTRA_CA_CERTS=/path/to/aide-ca.pem         # Node.js tools
REQUESTS_CA_BUNDLE=/path/to/aide-ca.pem          # Python tools
```

Real credentials: never in agent env.

---

## Configuration Surface

In `.aide.yaml`:

```yaml
credential_proxy:
  enabled: true                  # opt-in; default false until stable
  credentials:
    - domain: "api.anthropic.com"
      secret: anthropic-api-key  # aide secret name
    - domain: "*.googleapis.com"
      helper: "gcloud auth print-access-token"
    - domain: "*.amazonaws.com"
      helper: "aws sts get-session-token"
```

Built-in provider mappings (Anthropic, GCP, AWS) are pre-loaded when the
matching secret or helper is configured; custom domains use the explicit
`credentials:` list.

---

## Known Limitations (v1)

| Limitation | Detail | Workaround |
|---|---|---|
| gRPC-Python ALPN bug | grpcio + TLS-terminating proxy fails with ALPN mismatch (#32172, unresolved upstream) | Use REST-based GCP client libraries; document limitation |
| Claude Code (Bun) CA trust | `NODE_EXTRA_CA_CERTS` has regressions in some Bun versions | Permanent CA in system trust store (macOS keychain / Linux system store) covers Bun via `NODE_USE_SYSTEM_CA=1` |
| macOS Go tools `SSL_CERT_FILE` | Go proposal #77865 accepted but not shipped; `SSL_CERT_FILE` doesn't work for Go tools on macOS | Permanent CA in macOS keychain covers Go tools via Security.framework |

---

## Security Properties

| Property | Mechanism |
|---|---|
| Agent never sees real credentials | Dummy tokens only in env; real credentials in proxy token registry |
| Stolen dummy token is useless outside session | Token invalidated on session teardown; 128-bit random — brute-force infeasible |
| Proxy cannot be used by other processes | Unix socket + SO_PEERCRED caller verification |
| Authorization header never logged | Opaque log type; `[REDACTED]` in all formatters including debug |
| Credential helpers cannot be hijacked | Absolute binary paths; aide-controlled PATH; helper output piped direct to memory |
| Direct TCP bypass prevented | AIDE-dpe.6: sandbox allows outbound only to proxy socket |
| Credential injected only to correct host | Swap gated on TargetDomain matching CONNECT hostname; allowlist from Mode A Filter |
| CA private key secure | Encrypted at rest in aide config dir; never written to tmpfiles |

---

## Dependencies

| Dependency | Why |
|---|---|
| AIDE-dpe.6 | Network isolation — sandbox must deny direct outbound TCP except proxy socket. Without this the entire scheme provides no security guarantee. |
| AIDE-hze.1 | Defines the `Filter` + `Inspector` Go interfaces that the Credential Inspector implements |
| AIDE-hze.4 | Per-process CA generation and env wiring (`SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, etc.) |

The Credential Inspector **must not** ship before AIDE-dpe.6 and AIDE-hze.4.

---

## Out of Scope (v1)

- Body inspection or prompt-injection scanning (Mode B with mitmproxy/hetty)
- gRPC-Python support (upstream ALPN bug blocks it)
- Automatic credential rotation mid-session
- Per-account proxy tokens (single token per service per session is sufficient)
