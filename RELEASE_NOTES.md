## v3.6.2 (2026-11-14)

### 🐛 Bug Fixes

- Write `wire_api = "responses"` for gateways in Codex's `config.toml`. Codex 0.167 refuses to load a file containing `wire_api = "chat"`, so Codex did not start at all for profiles with a gateway marked for Codex. Entries written by older aide versions are reported as drift (`wire_api differs`) and rewritten by `aide gateway sync` or the next launch. A gateway used with Codex must now serve the OpenAI Responses API (`/v1/responses`).

### 🔧 Internal

- Update `go.opentelemetry.io/otel/sdk` (and the rest of the indirect `go.opentelemetry.io/otel` modules) from 1.45.0 to 1.47.0, and `go.yaml.in/yaml/v3` to 3.0.5.
