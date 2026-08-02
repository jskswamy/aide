## v2.0.3 — 2026-08-02

This is a security patch release. No new features or breaking changes.

### Security

#### Bump google.golang.org/grpc from v1.79.3 to v1.82.1

Addresses vulnerability `GO-2026-6061` in `google.golang.org/grpc`. The
vulnerability is reachable via `internal/launcher/runtime.go` through the
gRPC transport layer.

- Upgrades `google.golang.org/grpc` to `v1.82.1` which stops reading from
  connections flooded by HTTP/2 frames and fixes an xDS RBAC authorization
  bypass
