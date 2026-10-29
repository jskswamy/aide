# Design index

Index of design documents under `docs/designs/`. A new design is registered here when it is saved.

| Link | Date | Author | Components | Description |
| --- | --- | --- | --- | --- |
| [OpenCode profile database isolation](opencode-profile-database-isolation.md) | 2026-10-28 | jskswamy | launcher, harness, profile migrate, seatbelt, release notes | A profile that selects an OpenCode config dir opens its own database file in that directory, and commands that accept `--standalone` get a private server. Accounts are not copied. |
| [Multi-harness custom model gateway](gateway/design.md) | 2026-10-28 | jskswamy | gateway, credentials, harness config | `aide gateway` registers a custom model gateway and writes each harness's model catalogue without putting the bearer token on disk. |
