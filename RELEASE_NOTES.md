## v3.5.0 (2026-11-07)

### 🔧 Internal

- Decouple `pkg/seatbelt` (`guards` and `modules`) from `internal/fsutil` and `internal/homepath` by moving `ExpandHome`, `ResolveOrSelf`, `IsUnderDir`, and `ResolveWidening` into `pkg/seatbelt` and enforcing the boundary in `TestNoInternalImports`.
