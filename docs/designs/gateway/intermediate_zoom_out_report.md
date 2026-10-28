# EGM Intermediate Zoom Out Review (Stage 8): `docs/designs/gateway/design.md`

**Target Document:** [`docs/designs/gateway/design.md`](file:///Users/subramk/source/github.com/jskswamy/aide/docs/designs/gateway/design.md)
**Standard Applied:** `zoom-out-standard.md`

---

## Assessment of Architectural Additions & Mitigations

Following the Comprehension (Stage 6) and Critic/Threat Modeling (Stage 7) passes, the high-level architecture remains minimal and well-bounded provided the following simplicity guardrails are maintained when resolving Stage 6/7 findings:

1. **Pure Value Transformation for Gateway Credentials (`SynthesizeGatewayCredentials`)**:
   - Rather than adding shadow-tracking fields onto `config.Context` and filtering them out in `Context.MarshalYAML()`, follow `TranslateLegacyCredentials(ctx Context, store string) (Context, []LegacyCredential, error)` in `internal/config/credentials.go`. Returning a cloned `Context` during credential planning/validation keeps `Context.MarshalYAML()` trivial and prevents accidental persistence of synthesized routes.
2. **Single-Attempt Bounded HTTP Fetch (No Multi-Retry Loop on Launch)**:
   - Resolves both the Stage 6/7 timeout contradiction and launch latency risk by using a single `10s` `context.WithTimeout` (`4 MiB` `io.LimitReader`) with cross-origin/scheme-downgrade redirects blocked via `http.Client.CheckRedirect`.
3. **Reuse Existing Helpers (`fsutil.AtomicWrite`, `statedir.Path(homeDir, ...)`, `launchConfigDir`)**:
   - Avoids introducing parallel file-writing or directory-resolution code paths.
