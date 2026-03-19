# Epic 5: Secrets Lifecycle (P1) -- Tasks 19-22

**Date:** 2026-03-18
**Priority:** P1
**Dependencies:** T6 (age key discovery), T7 (sops decryption), T3 (config loader)
**Design Decisions:** DD-4, DD-6, DD-10, DD-13

> **Implementation Note -- sops v3 Go API:** This spec describes the *conceptual*
> sops v3 API; exact method signatures may differ. Before implementing, verify
> the signatures of `sops.Tree.Encrypt`, `UpdateMasterKeysWithKeyServices`,
> `GetDataKeyWithKeyServices`, and store methods against the actual sops source
> (`github.com/getsops/sops/v3`). Reference importers such as FluxCD's
> kustomize-controller and Terragrunt demonstrate correct usage patterns and are
> good cross-references for how the library is invoked in practice.

---

## Overview

This epic adds four subcommands under `aide secrets` that let users manage
sops-encrypted secrets files without ever invoking the `sops` CLI directly.
All operations use the sops Go library (DD-4). Encrypted files live under
`$XDG_CONFIG_HOME/aide/secrets/` alongside the main config (DD-6). Plaintext
is never written to persistent disk; all temporary plaintext lives in
`$XDG_RUNTIME_DIR` or an OS-equivalent tmpfs (DD-10). Age keys are discovered
through the priority chain: YubiKey, `$SOPS_AGE_KEY`, `$SOPS_AGE_KEY_FILE`,
default key file (DD-13).

### File Map

| File | Purpose |
|------|---------|
| `cmd/aide/secrets.go` | Cobra command tree: `secrets create`, `secrets edit`, `secrets list`, `secrets rotate` |
| `internal/secrets/manager.go` | Core logic for create, edit, list, rotate |
| `internal/secrets/manager_test.go` | Unit and integration tests for the manager |
| `internal/secrets/age.go` | Age key discovery (implemented in T6, consumed here) |
| `internal/secrets/sops.go` | Sops decrypt/encrypt helpers (implemented in T7, extended here) |
| `internal/config/paths.go` | `SecretsDir()` and `RuntimeDir()` helpers (implemented in T2/T9) |

---

## Task 19: `aide secrets create`

### Objective

Create a new sops-encrypted YAML secrets file, ensuring plaintext is never
written to persistent disk.

### CLI Interface

```
aide secrets create <name>
```

- `<name>` is required. It becomes the filename `<name>.enc.yaml`.
- If `<name>.enc.yaml` already exists, abort with an error:
  `"secrets/personal.enc.yaml already exists. Use 'aide secrets edit personal' to modify it."`

### Detailed Flow

```
1. Parse <name> argument; validate it is a safe filename (alphanumeric, hyphens,
   underscores only -- reject path separators, dots, etc.).

2. Compute target path:
     targetPath = filepath.Join(config.SecretsDir(), name+".enc.yaml")
   If targetPath exists, return error.

3. Discover age public key (call age.DiscoverPublicKey()):
   a. Check for YubiKey via age-plugin-yubikey identity.
   b. Check $SOPS_AGE_KEY env var -- derive public key from the secret key.
   c. Check $SOPS_AGE_KEY_FILE.
   d. Check $XDG_CONFIG_HOME/sops/age/keys.txt.
   e. If none found, return error:
      "No age identity found. Run 'aide setup' to generate one, or set
       SOPS_AGE_KEY / SOPS_AGE_KEY_FILE."

4. Create a secure temporary directory:
     tmpDir = os.MkdirTemp(runtimeDir(), "aide-secrets-")
     os.Chmod(tmpDir, 0700)
   where runtimeDir() returns $XDG_RUNTIME_DIR (Linux tmpfs) or the result of
   os.MkdirTemp("", ...) on macOS (see platform notes below).
   Register cleanup via defer + signal handler (SIGTERM, SIGINT, SIGQUIT, SIGHUP).

5. Write YAML template to temporary file:
     tmpFile = filepath.Join(tmpDir, name+".yaml")
   Template content:
     # aide secrets file: <name>
     # Add your secrets as key: value pairs.
     # These keys are referenced in config.yaml as {{ .secrets.<key> }}
     #
     # Example:
     #   anthropic_api_key: sk-ant-...
     #   openai_api_key: sk-...

6. Open $EDITOR (fall back to $VISUAL, then "vi") on tmpFile.
   Wait for editor to exit.

7. Read the edited tmpFile contents. If the file is empty or unchanged from the
   template, abort: "No secrets entered. Aborting."

8. Validate the content is valid YAML (yaml.v3 Unmarshal into map[string]any).
   Reject non-flat structures: all values must be strings or numbers.
   Error: "Secrets file must be a flat key-value YAML map."
   Note: The flat-structure restriction is intentional for v1 simplicity
   (easier validation, predictable template expansion, straightforward sops
   round-tripping). This can be relaxed in future versions if nested
   structures are needed.

9. Encrypt using sops library:
     import "github.com/getsops/sops/v3"
     import "github.com/getsops/sops/v3/aes"
     import "github.com/getsops/sops/v3/keys"
     import "github.com/getsops/sops/v3/keyservice"
     import agekeys "github.com/getsops/sops/v3/age"

   a. Parse the plaintext YAML into a sops.Tree.
   b. Build a sops.KeyGroup containing the discovered age recipient(s).
   c. Call tree.Encrypt(key, cipher) using aes.NewCipher().
   d. Serialize the encrypted tree to YAML bytes.

10. Ensure secrets directory exists:
      os.MkdirAll(config.SecretsDir(), 0700)

11. Write encrypted bytes to targetPath with mode 0600.

12. Remove tmpDir (deferred cleanup also fires as safety net).

13. Print: "Created secrets/<name>.enc.yaml (encrypted with age key <short-id>)"
```

### Platform Notes -- Secure Temporary Files

- **Linux:** `$XDG_RUNTIME_DIR` is guaranteed tmpfs (typically `/run/user/<uid>`).
  Use it directly.
- **macOS:** No standard tmpfs. Use `os.MkdirTemp("", ...)` which uses `$TMPDIR`
  (typically `/private/var/folders/.../T/`). This is on disk but the file lifetime
  is seconds and cleanup is aggressive. For maximum security, users can mount a
  RAM disk and set `$XDG_RUNTIME_DIR` to it. Document this in `aide setup` output.

### Error Handling

| Condition | Behavior |
|-----------|----------|
| Name contains path separators | Error: "Invalid secrets name. Use alphanumeric, hyphens, underscores only." |
| File already exists | Error with suggestion to use `edit` |
| No age key found | Error with setup guidance |
| Editor exits non-zero | Abort, clean up temp file |
| Invalid YAML entered | Error with parse details |
| Sops encryption fails | Error with sops message, clean up temp file |
| Signal during edit | Signal handler removes tmpDir before exit |

### TDD Test Descriptions

All tests go in `internal/secrets/manager_test.go`.

1. **TestCreate_HappyPath** -- Set up a fake `$XDG_CONFIG_HOME` and
   `$XDG_RUNTIME_DIR` in a temp directory. Provide a valid age key via
   `$SOPS_AGE_KEY`. Mock the editor by setting `$EDITOR` to a script that
   writes valid YAML to the file. Call `Manager.Create("test")`. Assert:
   - `secrets/test.enc.yaml` exists
   - File is valid sops-encrypted YAML (contains `sops:` metadata key)
   - Decrypting with the same age key yields the original plaintext
   - No plaintext files remain in runtime dir

2. **TestCreate_AlreadyExists** -- Pre-create `secrets/test.enc.yaml`. Call
   `Manager.Create("test")`. Assert error contains "already exists".

3. **TestCreate_InvalidName** -- Call with names `"../escape"`, `"has.dots"`,
   `"has/slash"`. Assert error for each.

4. **TestCreate_NoAgeKey** -- Unset all age key env vars, no key file. Assert
   error contains "No age identity found".

5. **TestCreate_EmptyEditor** -- Set `$EDITOR` to a no-op script (`true`).
   Assert error contains "No secrets entered".

6. **TestCreate_InvalidYAML** -- Set `$EDITOR` to a script that writes
   `[not: valid: flat: yaml`. Assert error about YAML parsing.

7. **TestCreate_CleanupOnError** -- Set `$EDITOR` to a script that writes
   invalid content. Assert runtime temp directory is cleaned up.

8. **TestCreate_CleanupOnSignal** -- Start Create in a goroutine, send
   SIGINT to the process during the editor wait, assert temp dir removed.
   (Integration test, can use `exec.Command` to run a subprocess.)

### Verification Commands

```bash
# Run unit tests
go test ./internal/secrets/ -run TestCreate -v

# Manual verification
export SOPS_AGE_KEY=$(age-keygen 2>/dev/null)
aide secrets create test-manual
# Verify file exists
ls -la ~/.config/aide/secrets/test-manual.enc.yaml
# Verify it decrypts
sops -d ~/.config/aide/secrets/test-manual.enc.yaml
# Verify no temp files remain
ls /run/user/$(id -u)/aide-secrets-* 2>/dev/null && echo "LEAK" || echo "OK: clean"
```

---

## Task 20: `aide secrets edit`

### Objective

Decrypt an existing secrets file to a secure temp file, open it in `$EDITOR`,
re-encrypt on save. The temp file must be removed even if the editor crashes or
the user sends a signal.

### CLI Interface

```
aide secrets edit <name>
```

- `<name>` is required. Resolves to `<name>.enc.yaml` in the secrets directory.

### Detailed Flow

```
1. Parse and validate <name> (same rules as create).

2. Compute source path:
     srcPath = filepath.Join(config.SecretsDir(), name+".enc.yaml")
   If srcPath does not exist, return error:
     "secrets/<name>.enc.yaml not found. Use 'aide secrets create <name>' to create it."

3. Decrypt using sops library:
     import "github.com/getsops/sops/v3/decrypt"
     plaintext, err := decrypt.File(srcPath, "yaml")
   This uses the standard age key discovery chain (DD-13).

4. Create secure temp directory (same as create flow):
     tmpDir = os.MkdirTemp(runtimeDir(), "aide-secrets-")
     os.Chmod(tmpDir, 0700)

5. Register cleanup function:
     cleanup := func() {
         os.RemoveAll(tmpDir)
     }
     defer cleanup()
     // Also register signal handlers for SIGTERM, SIGINT, SIGQUIT, SIGHUP
     // that call cleanup() then os.Exit(1).

6. Write plaintext to temp file:
     tmpFile = filepath.Join(tmpDir, name+".yaml")
     os.WriteFile(tmpFile, plaintext, 0600)

7. Record modification time of tmpFile before opening editor:
     beforeStat, _ := os.Stat(tmpFile)

8. Open $EDITOR on tmpFile. Wait for exit.
   If editor exits non-zero, abort (cleanup runs via defer).

9. Check if file was modified:
     afterStat, _ := os.Stat(tmpFile)
   If mtime is unchanged AND content is byte-identical, print
   "No changes made." and exit cleanly (cleanup runs).

10. Read modified content. Validate as flat YAML (same as create).

11. Re-encrypt:
    a. Read the original encrypted file to extract the existing sops metadata
       (key groups, recipients list). This preserves the recipient configuration.
    b. Parse new plaintext into a sops.Tree, apply the existing key groups.
    c. Encrypt with aes.NewCipher().
    d. Serialize to YAML.

12. Write encrypted bytes back to srcPath (atomic write: write to srcPath+".tmp",
    then os.Rename).

13. Cleanup runs (defer). Print:
    "Updated secrets/<name>.enc.yaml"
```

### Key Design Point: Preserving Recipients

When re-encrypting, the existing sops metadata (stored in the encrypted file)
contains the list of age recipients. The edit flow must preserve this list --
the user is editing values, not changing who can decrypt. To do this:

```go
// Load the encrypted sops.Tree (not decrypted -- just the structure)
encTree, err := loadEncryptedTree(srcPath)
// Extract KeyGroups from encTree.Metadata
keyGroups := encTree.Metadata.KeyGroups
// Build a new tree from the edited plaintext, apply the same keyGroups
newTree := buildTree(editedPlaintext, keyGroups)
newTree.Encrypt(dataKey, cipher)
```

### Error Handling

| Condition | Behavior |
|-----------|----------|
| File not found | Error with `create` suggestion |
| Decryption fails (wrong key) | Error: "Failed to decrypt ... Is your YubiKey plugged in?" |
| Editor exits non-zero | Abort, clean up |
| New content is invalid YAML | Error, clean up, original file unchanged |
| Write failure (disk full, permissions) | Error, original file unchanged (atomic write) |
| SIGINT during edit | Signal handler removes tmpDir |

### TDD Test Descriptions

1. **TestEdit_HappyPath** -- Create an encrypted file via `Manager.Create`.
   Set `$EDITOR` to a script that appends a new key-value line. Call
   `Manager.Edit("test")`. Decrypt the result and assert the new key exists
   alongside original keys.

2. **TestEdit_NoChanges** -- Set `$EDITOR` to a no-op (opens and closes without
   saving). Assert the output contains "No changes made" and the encrypted file
   is byte-identical to before.

3. **TestEdit_FileNotFound** -- Call `Manager.Edit("nonexistent")`. Assert
   error contains "not found".

4. **TestEdit_PreservesRecipients** -- Create a file with two age recipients.
   Edit it. Decrypt the result and verify the sops metadata still lists both
   recipients.

5. **TestEdit_InvalidYAMLRejected** -- Set `$EDITOR` to write broken YAML.
   Assert error, assert original file is unchanged.

6. **TestEdit_AtomicWrite** -- Simulate a write failure (read-only secrets
   dir). Assert original file is intact.

7. **TestEdit_CleanupOnEditorCrash** -- Set `$EDITOR` to a script that exits
   with code 1. Assert temp dir is cleaned up and original file is unchanged.

### Verification Commands

```bash
go test ./internal/secrets/ -run TestEdit -v

# Manual verification
aide secrets create edit-test
aide secrets edit edit-test
# Verify round-trip
sops -d ~/.config/aide/secrets/edit-test.enc.yaml
# Verify no temp files
ls /run/user/$(id -u)/aide-secrets-* 2>/dev/null && echo "LEAK" || echo "OK: clean"
```

---

## Task 21: `aide secrets list`

### Objective

List all encrypted secrets files and show which contexts reference each one.

### CLI Interface

```
aide secrets list
```

No arguments. Output example:

```
NAME            CONTEXTS         RECIPIENTS
personal        personal, oss    age1abc...def (primary), age1xyz...789
work            work             age1abc...def (primary)
unused          (none)           age1abc...def (primary)
```

### Detailed Flow

```
1. Compute secrets directory:
     secretsDir = config.SecretsDir()
   If the directory does not exist, print "No secrets directory found." and exit.

2. Scan for *.enc.yaml files:
     entries, err := os.ReadDir(secretsDir)
   Filter to files matching *.enc.yaml. Extract name by trimming the suffix.

3. Load config.yaml (if it exists) and build a reverse map:
     contextsBySecretsFile map[string][]string
   Iterate over all contexts. For each context with a secrets_file field, add
   the context name to the map entry for that filename.
   Also handle minimal (flat) config format -- if secrets_file is set at the
   top level, map it to "(default)".

4. For each encrypted file, extract recipient information:
   a. Read the file and parse sops metadata without decrypting.
      Use sops library to load the tree structure:
        import "github.com/getsops/sops/v3/stores/yaml"
        store := yaml.Store{}
        tree, err := store.LoadEncryptedFile(fileBytes)
      Extract tree.Metadata.KeyGroups -- each group contains age keys.
   b. Collect the age public key fingerprints/short IDs.

5. Format and print the table. Use tabwriter for alignment:
     w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
```

### Error Handling

| Condition | Behavior |
|-----------|----------|
| No secrets directory | Print "No secrets directory found. Use 'aide secrets create' to get started." |
| Empty secrets directory | Print "No secrets files found." |
| Corrupt encrypted file | Show name with "(error reading metadata)" in recipients column |
| No config.yaml | Show "(no config)" in contexts column for all files |

### TDD Test Descriptions

1. **TestList_HappyPath** -- Create two encrypted files and a config with
   contexts referencing them. Call `Manager.List()`. Assert returned list
   contains both files with correct context associations.

2. **TestList_NoSecretsDir** -- Point config to a nonexistent directory. Assert
   graceful message, no error.

3. **TestList_UnreferencedFile** -- Create an encrypted file not referenced by
   any context. Assert it appears with empty contexts list.

4. **TestList_MultipleContextsSameFile** -- Two contexts reference the same
   secrets file. Assert both context names appear.

5. **TestList_MinimalConfig** -- Use flat config format with `secrets_file`.
   Assert it maps to "(default)" context.

6. **TestList_RecipientsExtracted** -- Create a file with known age recipients.
   Assert the list output includes the correct public key short IDs.

7. **TestList_CorruptFile** -- Place a non-sops YAML file with `.enc.yaml`
   extension. Assert it appears with an error note, does not crash.

### Verification Commands

```bash
go test ./internal/secrets/ -run TestList -v

# Manual verification
aide secrets create one
aide secrets create two
aide secrets list
```

---

## Task 22: `aide secrets rotate`

### Objective

Add or remove age recipients from an existing encrypted secrets file without
exposing plaintext to disk. This enables team onboarding/offboarding and
multi-device setups.

### CLI Interface

```
aide secrets rotate <name> --add-key <age-public-key>
aide secrets rotate <name> --remove-key <age-public-key>
aide secrets rotate <name> --add-key <key1> --add-key <key2>   # multiple
aide secrets rotate <name> --remove-key <key1> --add-key <key2> # combo
```

- `--add-key` and `--remove-key` are repeatable string slice flags.
- At least one of the two flags is required.

### Detailed Flow

```
1. Parse and validate <name>. Resolve to srcPath.
   If file does not exist, error.

2. Validate provided public keys:
   - Each --add-key value must be a valid age public key (starts with "age1"
     and is valid bech32).
   - Each --remove-key value must match an existing recipient in the file.
   Parse age public keys:
     import "filippo.io/age"
     _, err := age.ParseX25519Recipient(keyStr)

3. Load the encrypted sops tree (without decrypting to disk):
     fileBytes, _ := os.ReadFile(srcPath)
     store := yamlstore.Store{}
     tree, _ := store.LoadEncryptedFile(fileBytes)

4. Modify the recipient list:
   a. Extract current age recipients from tree.Metadata.KeyGroups.
   b. For each --remove-key:
      - Find the matching age key in the key groups.
      - If not found, error: "Key <short-id> is not a recipient of this file."
      - Remove it.
      - Validate that at least one recipient remains after all removals.
        Error: "Cannot remove all recipients. At least one age key must remain."
   c. For each --add-key:
      - If the key is already a recipient, warn: "Key <short-id> is already a
        recipient. Skipping."
      - Otherwise, add it to the first key group.

5. Re-encrypt the data key for the new set of recipients.
   This is the critical step -- sops supports updating recipients without
   decrypting the data:

     // Decrypt the data key using our local age identity
     dataKey, err := tree.Metadata.GetDataKeyWithKeyServices(
         []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
     )

     // Update the key groups with the modified recipient list
     tree.Metadata.KeyGroups = updatedKeyGroups

     // Re-encrypt the data key for all (new) recipients
     errs := tree.Metadata.UpdateMasterKeysWithKeyServices(
         dataKey,
         []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
     )

   The actual data stays encrypted with the same data key. Only the data key's
   wrapping changes (re-wrapped for the new recipient set).

6. Serialize the updated tree back to YAML:
     output, _ := store.EmitEncryptedFile(tree)

7. Atomic write to srcPath (write to .tmp, rename).

8. Print summary:
     "Rotated secrets/<name>.enc.yaml"
     "  Added: age1abc...def"
     "  Removed: age1xyz...789"
     "  Current recipients (2): age1abc...def, age1mmm...nnn"
```

### Key Design Point: No Plaintext Exposure

The rotate operation NEVER decrypts the actual secret values. It only decrypts
the data key (a symmetric AES key) in memory, then re-encrypts that same data
key for the updated set of age recipients. The secret values themselves remain
encrypted throughout. This is a sops-native operation -- the data key wrapping
is independent of the data encryption.

### Error Handling

| Condition | Behavior |
|-----------|----------|
| File not found | Error with `create` suggestion |
| Invalid age public key format | Error: "Invalid age public key: <value>" |
| --remove-key not a current recipient | Error identifying the key |
| Removing all recipients | Error: "Cannot remove all recipients" |
| --add-key already a recipient | Warning (not error), skip |
| Neither --add-key nor --remove-key | Error: "Specify --add-key or --remove-key" |
| Decryption of data key fails | Error: "Cannot rotate -- unable to decrypt data key. Is your age key a recipient?" |

### TDD Test Descriptions

1. **TestRotate_AddKey** -- Create an encrypted file with one recipient. Call
   `Manager.Rotate("test", addKeys=["age1new..."], removeKeys=nil)`. Load the
   result and assert two recipients in sops metadata. Decrypt with the original
   key to verify data is intact.

2. **TestRotate_RemoveKey** -- Create with two recipients. Remove one. Assert
   one recipient remains. Decrypt with the remaining key succeeds. Decrypt
   with the removed key fails.

3. **TestRotate_AddAndRemove** -- Add one key, remove another in the same call.
   Assert the final recipient list is correct.

4. **TestRotate_RemoveLastKey** -- Try to remove the only recipient. Assert
   error "Cannot remove all recipients".

5. **TestRotate_AddDuplicate** -- Add a key that is already a recipient. Assert
   warning is emitted, recipient list unchanged.

6. **TestRotate_InvalidKeyFormat** -- Pass `"not-a-valid-key"` as --add-key.
   Assert error about invalid format.

7. **TestRotate_RemoveNonexistent** -- Pass a valid age public key that is not
   a recipient. Assert error.

8. **TestRotate_DataIntegrity** -- Create file with known plaintext, rotate
   (add a key), decrypt, assert plaintext is identical to original.

9. **TestRotate_NoPlaintextOnDisk** -- Monitor the filesystem (or mock it)
   during rotate. Assert no plaintext file is created anywhere. The rotate
   operation should be entirely in-memory.

10. **TestRotate_FileNotFound** -- Call rotate on nonexistent file. Assert error.

### Verification Commands

```bash
go test ./internal/secrets/ -run TestRotate -v

# Manual verification
# Generate two age keys
KEY1=$(age-keygen 2>&1 | grep "public key:" | awk '{print $3}')
KEY2=$(age-keygen 2>&1 | grep "public key:" | awk '{print $3}')

aide secrets create rotate-test
aide secrets rotate rotate-test --add-key "$KEY2"
aide secrets list  # should show 2 recipients for rotate-test

aide secrets rotate rotate-test --remove-key "$KEY2"
aide secrets list  # should show 1 recipient
```

---

## Cobra Command Structure (`cmd/aide/secrets.go`)

```go
package main

import "github.com/spf13/cobra"

func newSecretsCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "secrets",
        Short: "Manage encrypted secrets files",
        Long: `Create, edit, list, and rotate encrypted secrets files.

Secrets are stored as sops-encrypted YAML in $XDG_CONFIG_HOME/aide/secrets/.
Plaintext never touches persistent disk.`,
    }

    cmd.AddCommand(
        newSecretsCreateCmd(),
        newSecretsEditCmd(),
        newSecretsListCmd(),
        newSecretsRotateCmd(),
    )
    return cmd
}

func newSecretsCreateCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "create <name>",
        Short: "Create a new encrypted secrets file",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            mgr := secrets.NewManager(/* config paths */)
            return mgr.Create(args[0])
        },
    }
}

func newSecretsEditCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "edit <name>",
        Short: "Decrypt, edit, and re-encrypt a secrets file",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            mgr := secrets.NewManager(/* config paths */)
            return mgr.Edit(args[0])
        },
    }
}

func newSecretsListCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "list",
        Short: "List available secrets files and their contexts",
        Args:  cobra.NoArgs,
        RunE: func(cmd *cobra.Command, args []string) error {
            mgr := secrets.NewManager(/* config paths */)
            return mgr.List(cmd.OutOrStdout())
        },
    }
}

func newSecretsRotateCmd() *cobra.Command {
    var addKeys, removeKeys []string
    cmd := &cobra.Command{
        Use:   "rotate <name>",
        Short: "Add or remove age recipients from a secrets file",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if len(addKeys) == 0 && len(removeKeys) == 0 {
                return fmt.Errorf("specify --add-key or --remove-key")
            }
            mgr := secrets.NewManager(/* config paths */)
            return mgr.Rotate(args[0], addKeys, removeKeys)
        },
    }
    cmd.Flags().StringSliceVar(&addKeys, "add-key", nil, "Age public key to add as recipient")
    cmd.Flags().StringSliceVar(&removeKeys, "remove-key", nil, "Age public key to remove as recipient")
    return cmd
}
```

---

## Manager Interface (`internal/secrets/manager.go`)

```go
package secrets

import "io"

// Manager handles the full lifecycle of sops-encrypted secrets files.
type Manager struct {
    secretsDir string // $XDG_CONFIG_HOME/aide/secrets/
    runtimeDir string // $XDG_RUNTIME_DIR or OS temp
    configPath string // $XDG_CONFIG_HOME/aide/config.yaml (for list context mapping)
    ageDiscoverer AgeDiscoverer // from age.go (T6)
}

func NewManager(secretsDir, runtimeDir, configPath string, ad AgeDiscoverer) *Manager

// Create creates a new encrypted secrets file.
// Opens $EDITOR with a YAML template, encrypts the result.
func (m *Manager) Create(name string) error

// Edit decrypts a secrets file to a temp file, opens $EDITOR,
// re-encrypts on save, removes temp file on exit.
func (m *Manager) Edit(name string) error

// List writes a table of secrets files, their referencing contexts,
// and recipients to the given writer.
func (m *Manager) List(w io.Writer) error

// Rotate adds and/or removes age recipients from a secrets file
// without decrypting the secret values to disk.
func (m *Manager) Rotate(name string, addKeys, removeKeys []string) error
```

### Internal Helpers

```go
// validateName checks that a secrets name is safe for use as a filename.
func validateName(name string) error

// resolveEditor returns the editor command from $EDITOR, $VISUAL, or "vi".
func resolveEditor() string

// secureTempDir creates a 0700 temp directory inside runtimeDir.
// Returns the path and a cleanup function.
func (m *Manager) secureTempDir(prefix string) (string, func(), error)

// loadEncryptedTree loads a sops tree from an encrypted file without decrypting.
func loadEncryptedTree(path string) (*sops.Tree, error)

// encryptTree encrypts a plaintext YAML map into a sops tree with the given key groups.
func encryptTree(plaintext []byte, keyGroups []sops.KeyGroup) (*sops.Tree, error)

// atomicWrite writes data to a temp file then renames to the target path.
func atomicWrite(path string, data []byte, mode os.FileMode) error
```

---

## Cross-Cutting Concerns

### Signal Handling

All operations that create temp files must register signal handlers. Use a
shared pattern:

```go
func withCleanup(cleanupFn func(), body func() error) error {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)
    done := make(chan struct{})

    go func() {
        select {
        case <-sigs:
            cleanupFn()
            os.Exit(1)
        case <-done:
        }
    }()

    defer func() {
        close(done)
        signal.Stop(sigs)
        cleanupFn()
    }()

    return body()
}
```

### Sops Library Usage Reference

The sops Go library is used differently across the four tasks. Summary:

| Task | Sops Operation | Key API |
|------|---------------|---------|
| Create | Encrypt plaintext | `sops.Tree.Encrypt()`, `aes.NewCipher()` |
| Edit | Decrypt file, re-encrypt | `decrypt.File()`, `sops.Tree.Encrypt()` |
| List | Read metadata only | `yamlstore.Store{}.LoadEncryptedFile()` |
| Rotate | Re-wrap data key | `Tree.Metadata.GetDataKeyWithKeyServices()`, `UpdateMasterKeysWithKeyServices()` |

Import paths:
- `github.com/getsops/sops/v3`
- `github.com/getsops/sops/v3/aes`
- `github.com/getsops/sops/v3/decrypt`
- `github.com/getsops/sops/v3/stores/yaml`
- `github.com/getsops/sops/v3/keyservice`
- `github.com/getsops/sops/v3/keys`
- `filippo.io/age` (for public key validation in rotate)

### Testing Infrastructure

All tests share a test helper:

```go
// testManager creates a Manager with isolated temp directories
// and a known age key for testing.
func testManager(t *testing.T) (*Manager, string) {
    t.Helper()
    tmpDir := t.TempDir()
    secretsDir := filepath.Join(tmpDir, "secrets")
    runtimeDir := filepath.Join(tmpDir, "runtime")
    os.MkdirAll(secretsDir, 0700)
    os.MkdirAll(runtimeDir, 0700)

    // Generate ephemeral age key for tests
    key, _ := age.GenerateX25519Identity()
    t.Setenv("SOPS_AGE_KEY", key.String())

    configPath := filepath.Join(tmpDir, "config.yaml")
    ad := NewAgeDiscoverer()
    mgr := NewManager(secretsDir, runtimeDir, configPath, ad)
    return mgr, tmpDir
}

// mockEditor sets $EDITOR to a script that writes the given content.
func mockEditor(t *testing.T, content string) {
    t.Helper()
    script := filepath.Join(t.TempDir(), "editor.sh")
    os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\ncat > \"$1\" << 'ENDOFCONTENT'\n%s\nENDOFCONTENT\n", content)), 0755)
    t.Setenv("EDITOR", script)
}
```

---

## Design Decisions Referenced

- **DD-4 (Sops as Go Library):** All four tasks use the sops Go library, not
  the CLI. This eliminates `sops` binary as a runtime dependency. The library
  is well-tested (79+ importers including FluxCD, Terragrunt).

- **DD-6 (Single XDG Directory):** Secrets files live under
  `$XDG_CONFIG_HOME/aide/secrets/`, co-located with `config.yaml`. This enables
  `git init` on the entire `aide/` directory for reproducibility. Encrypted
  files are safe to commit.

- **DD-10 (Ephemeral Runtime Security):** Plaintext secrets exist only in
  `$XDG_RUNTIME_DIR` temp files (tmpfs on Linux) with aggressive cleanup via
  defer + signal handlers. On macOS, users are guided to use a RAM disk for
  maximum security. The rotate operation avoids plaintext entirely.

- **DD-13 (Age Key Discovery):** The `age.DiscoverPublicKey()` function (from
  T6) supports YubiKey, `$SOPS_AGE_KEY`, `$SOPS_AGE_KEY_FILE`, and the default
  key file. All four tasks use this discovery chain. Create and rotate need the
  public key; edit needs the private key (for decryption); list only reads
  metadata.
