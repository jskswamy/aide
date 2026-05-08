## v1.9.1

### 🔒 Security

- Bumped `go` directive from 1.25.7 to 1.25.10 so the toolchain pulls
  the patched `html/template`, `net`, and `net/http` (clears
  GO-2026-4982, GO-2026-4980, GO-2026-4971, and GO-2026-4918 reachable
  through `secrets.Rotate` and `launcher.RuntimeDir.Cleanup`).
- Upgraded `golang.org/x/net` from v0.51.0 to v0.53.0, fixing the
  HTTP/2 transport infinite loop on bad `SETTINGS_MAX_FRAME_SIZE`.
- No code changes; practical exploit risk for aide is low (no HTML
  rendering, macOS-only target), but the bump clears govulncheck and
  downstream SBOM scanners.
