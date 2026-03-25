# Home Metadata Traversal Design

**Goal:** Allow `file-read-metadata` (stat/lstat) across all of `$HOME` so POSIX tools can traverse parent directories and siblings of the project root without exposing file contents.

**Problem:** The split-read sandbox model scopes `$HOME` reads to specific development directories. Tools like `golangci-lint`, `git`, and `realpath` need to `lstat()` paths between `$HOME` and the project root (e.g., `~/source/`, `~/source/github.com/`). These paths are currently blocked, causing tool failures.

## Change

Replace the limited `file-read-metadata` literals:

```
(allow file-read-metadata
    (literal "$HOME")
    (literal "$HOME/Library"))
```

With a broad subpath:

```
(allow file-read-metadata
    (subpath "$HOME"))
```

## What `file-read-metadata` Exposes

- File/directory existence
- Size, timestamps, permissions, owner/group
- Inode number, file type
- Symlink target path

## What It Does NOT Expose

- File contents (`file-read-data`)
- Directory listings (`file-read-data` on directories)

## Threat Model

**Verified empirically** via `test-home-metadata.sh`:
- `stat` on any `$HOME` path: succeeds (metadata only)
- `cat` on file under `$HOME`: denied (no `file-read-data`)
- `ls` on directory under `$HOME`: denied (no `file-read-data`)

**Deny guard interaction:** No deny guard uses `deny file-read-metadata` or `deny file-read*` on `$HOME` paths. All deny guards use `deny file-read-data` (content only). No deny-wins conflict.

**Comparison with prior state:** Before the split-read model, the sandbox allowed `(allow file-read* (subpath "$HOME"))` — full content reads. This change allows only metadata, which is strictly more restrictive.

**Acceptable risk:** An agent could discover file/directory existence under `$HOME` via recursive `stat()`. File existence is not the protected asset — file contents are.

## Files Changed

- `pkg/seatbelt/guards/guard_filesystem.go` — replace metadata literals with subpath

## Testing

- Existing contract test `TestContract_ScopedHomeReads` verifies scoped reads still work
- Integration test `TestSandbox_HomeDocumentsNotReadable` verifies content reads are still blocked
- No new tests needed — broadening an allow doesn't break existing assertions
