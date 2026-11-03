# Design index

Index of design documents under `docs/designs/`. A new design is registered here when it is saved.

| Link | Date | Author | Components | Description |
| --- | --- | --- | --- | --- |
| [OpenCode profile database isolation](opencode-profile-database-isolation.md) | 2026-10-28 | jskswamy | launcher, harness, profile migrate, seatbelt, release notes | A profile that selects an OpenCode config dir opens its own database file in that directory, and commands that accept `--standalone` get a private server. Accounts are not copied. |
| [Multi-harness custom model gateway](gateway/design.md) | 2026-10-28 | jskswamy | gateway, credentials, harness config | `aide gateway` registers a custom model gateway and writes each harness's model catalogue without putting the bearer token on disk. |
| [Modular multi-file configuration layout](../superpowers/specs/2026-11-01-modular-config-layout-design.md) | 2026-11-01 | jskswamy | config, writer, migrate, provision, cmd/aide | Splits `~/.config/aide/config.yaml` into modular files (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, `profiles/<name>.yaml`) with backward-compatible loading, byte-diff writes, `aide config migrate` support, and a 7-day migration nudge. |
| [Interactive remote scope selection & conflict resolution for profile binding](profile-bind-remote-scope.md) | 2026-11-01 | jskswamy | profile bind, profile create, empty-state prompt, context resolver | `aide profile bind` and `aide profile create` offer interactive repo/team/org/path scope selection, `--scope` non-interactive targeting, and pre-persist cross-profile conflict resolution. |
