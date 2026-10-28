package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/yaml"
	"github.com/jskswamy/aide/internal/fsutil"
	yamlpkg "gopkg.in/yaml.v3"
)

// namePattern matches valid secret names: alphanumeric, hyphens, underscores.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// yamlTemplate is the template written to temp files for the editor.
const yamlTemplate = `# aide secrets file: %s
# Add your secrets as key: value pairs.
# These keys are referenced in config.yaml as {{ .secrets.<key> }}
#
# Example:
#   anthropic_api_key: sk-ant-...
#   openai_api_key: sk-...
`

// Manager handles the full lifecycle of sops-encrypted secrets files.
type Manager struct {
	secretsDir string
	runtimeDir string
	editor     EditorRunner
}

// NewManager creates a new secrets Manager.
func NewManager(secretsDir, runtimeDir string) *Manager {
	return &Manager{
		secretsDir: secretsDir,
		runtimeDir: runtimeDir,
		editor:     &RealEditorRunner{},
	}
}

// NewManagerWithEditor creates a Manager with a custom EditorRunner (for testing).
func NewManagerWithEditor(secretsDir, runtimeDir string, editor EditorRunner) *Manager {
	return &Manager{
		secretsDir: secretsDir,
		runtimeDir: runtimeDir,
		editor:     editor,
	}
}

// Create creates a new encrypted secrets file by opening $EDITOR.
func (m *Manager) Create(name, secretsDir, agePublicKey string) error {
	if err := validateName(name); err != nil {
		return err
	}

	targetPath := filepath.Join(secretsDir, name+".enc.yaml")
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("secrets/%s.enc.yaml already exists, use 'aide secrets edit %s' to modify it", name, name)
	}

	// Create secure temp directory.
	tmpDir, cleanup, err := m.secureTempDir("aide-secrets-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer cleanup()

	// Write template to temp file.
	tmpFile := filepath.Join(tmpDir, name+".yaml")
	template := fmt.Sprintf(yamlTemplate, name)
	if err := os.WriteFile(tmpFile, []byte(template), 0o600); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}

	// Open editor.
	editorBin := resolveEditor()
	if err := m.editor.Run(editorBin, []string{tmpFile}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read edited content.
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	return m.CreateFromContent(name, secretsDir, agePublicKey, content)
}

// CreateFromContent creates an encrypted secrets file from the given plaintext
// content bytes. This is the testable core that skips the editor interaction.
func (m *Manager) CreateFromContent(name, secretsDir, agePublicKey string, content []byte) error {
	if err := validateName(name); err != nil {
		return err
	}

	targetPath := filepath.Join(secretsDir, name+".enc.yaml")
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("secrets/%s.enc.yaml already exists, use 'aide secrets edit %s' to modify it", name, name)
	}

	// Validate content is not empty or comment-only.
	if err := validateContent(content); err != nil {
		return err
	}

	// Validate flat YAML.
	if err := validateFlatYAML(content); err != nil {
		return err
	}

	// Encrypt with sops.
	encrypted, err := encryptWithAge(content, agePublicKey)
	if err != nil {
		return fmt.Errorf("sops encryption failed: %w", err)
	}

	// Ensure secrets directory exists.
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Write encrypted file atomically. AtomicWrite gives tmp+rename
	// durability so a crash mid-write can't leave a partial AES blob
	// that fails to decrypt later.
	if err := fsutil.AtomicWrite(targetPath, encrypted); err != nil {
		return fmt.Errorf("failed to write encrypted file: %w", err)
	}

	return nil
}

// validateName checks that a secrets name is safe for use as a filename.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("invalid secrets name: name cannot be empty")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid secrets name %q, use alphanumeric, hyphens, underscores only", name)
	}
	return nil
}

// validateContent checks that content is not empty or comment-only.
func validateContent(content []byte) error {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return fmt.Errorf("no secrets entered, aborting")
	}

	// Check if all non-empty lines are comments.
	hasNonComment := false
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			hasNonComment = true
			break
		}
	}
	if !hasNonComment {
		return fmt.Errorf("no secrets entered, aborting")
	}

	return nil
}

// validateFlatYAML validates that content is a flat key-value YAML map.
func validateFlatYAML(content []byte) error {
	var raw interface{}
	if err := yamlpkg.Unmarshal(content, &raw); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	m, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("secrets file must be a flat key-value YAML map")
	}

	for k, v := range m {
		switch v.(type) {
		case string, int, int64, float64, bool, nil:
			// OK: scalar types.
		default:
			return fmt.Errorf("secrets file must be a flat key-value YAML map (key %q has non-scalar value)", k)
		}
	}

	return nil
}

// encryptWithAge encrypts plaintext YAML content using sops with the given
// age public key as the recipient.
func encryptWithAge(plaintext []byte, agePublicKey string) ([]byte, error) {
	store := &yaml.Store{}

	// Parse plaintext into sops branches.
	branches, err := store.LoadPlainFile(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML for encryption: %w", err)
	}

	// Build age key group.
	ageKey, err := age.MasterKeyFromRecipient(agePublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid age public key: %w", err)
	}

	keyGroup := sops.KeyGroup{ageKey}

	// Create sops tree.
	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{
			KeyGroups:         []sops.KeyGroup{keyGroup},
			UnencryptedSuffix: "_unencrypted",
			Version:           "3.12.2",
		},
	}

	// Generate a data key and encrypt.
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(
		[]keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	)
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to generate data key: %v", errs)
	}

	cipher := aes.NewCipher()
	unencryptedMac, err := tree.Encrypt(dataKey, cipher)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}

	// Encrypt the MAC itself with the data key.
	encryptedMac, err := cipher.Encrypt(unencryptedMac, dataKey, tree.Metadata.LastModified.Format("2006-01-02T15:04:05Z"))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt MAC: %w", err)
	}
	tree.Metadata.MessageAuthenticationCode = encryptedMac

	// Update master keys with the encrypted data key.
	errs2 := tree.Metadata.UpdateMasterKeysWithKeyServices(
		dataKey,
		[]keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	)
	if len(errs2) > 0 {
		return nil, fmt.Errorf("failed to update master keys: %v", errs2)
	}

	// Serialize to YAML.
	encrypted, err := store.EmitEncryptedFile(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize encrypted file: %w", err)
	}

	return encrypted, nil
}

// Edit decrypts a secrets file to a secure temp file, opens $EDITOR,
// re-encrypts on save, and removes the temp file on exit.
func (m *Manager) Edit(name, secretsDir string) error {
	if err := validateName(name); err != nil {
		return err
	}

	srcPath := filepath.Join(secretsDir, name+".enc.yaml")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("secrets/%s.enc.yaml not found, use 'aide secrets create %s' to create it", name, name)
	}

	// Decrypt using sops.
	identity, err := DiscoverAgeKey()
	if err != nil {
		return err
	}
	secrets, err := DecryptSecretsFile(srcPath, identity)
	if err != nil {
		return fmt.Errorf("failed to decrypt %s: %w", name, err)
	}

	// Build plaintext YAML from secrets.
	var plainBuilder strings.Builder
	for k, v := range secrets {
		plainBuilder.WriteString(k + ": " + v + "\n")
	}
	plaintext := []byte(plainBuilder.String())

	// Create secure temp directory.
	tmpDir, cleanup, err := m.secureTempDir("aide-secrets-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer cleanup()

	// Write plaintext to temp file.
	tmpFile := filepath.Join(tmpDir, name+".yaml")
	if err := os.WriteFile(tmpFile, plaintext, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Open editor.
	editorBin := resolveEditor()
	if err := m.editor.Run(editorBin, []string{tmpFile}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read edited content.
	newContent, err := os.ReadFile(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	return m.EditFromContent(name, secretsDir, newContent)
}

// EditFromContent re-encrypts a secrets file with newContent, preserving
// the original recipients. This is the testable core that skips the editor.
func (m *Manager) EditFromContent(name, secretsDir string, newContent []byte) error {
	if err := validateName(name); err != nil {
		return err
	}

	srcPath := filepath.Join(secretsDir, name+".enc.yaml")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("secrets/%s.enc.yaml not found, use 'aide secrets create %s' to create it", name, name)
	}

	// Validate content.
	if err := validateContent(newContent); err != nil {
		return err
	}
	if err := validateFlatYAML(newContent); err != nil {
		return err
	}

	// Create secure temp dir for the intermediate work.
	tmpDir, cleanup, err := m.secureTempDir("aide-secrets-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer cleanup()

	// Write new content to secure temp file (for potential debugging; mostly
	// for consistency with the Edit flow).
	tmpFile := filepath.Join(tmpDir, name+".yaml")
	if err := os.WriteFile(tmpFile, newContent, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Re-encrypt with the same recipients.
	return reencryptInPlace(srcPath, newContent)
}

// encryptWithKeyGroups encrypts plaintext YAML content using sops with the
// given key groups (preserving recipients from an existing file).
func encryptWithKeyGroups(plaintext []byte, keyGroups []sops.KeyGroup) ([]byte, error) {
	store := &yaml.Store{}

	// Parse plaintext into sops branches.
	branches, err := store.LoadPlainFile(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML for encryption: %w", err)
	}

	// Create sops tree with the existing key groups.
	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{
			KeyGroups:         keyGroups,
			UnencryptedSuffix: "_unencrypted",
			Version:           "3.12.2",
		},
	}

	// Generate a data key and encrypt.
	svcs := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(svcs)
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to generate data key: %v", errs)
	}

	cipher := aes.NewCipher()
	unencryptedMac, err := tree.Encrypt(dataKey, cipher)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}

	encryptedMac, err := cipher.Encrypt(unencryptedMac, dataKey, tree.Metadata.LastModified.Format("2006-01-02T15:04:05Z"))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt MAC: %w", err)
	}
	tree.Metadata.MessageAuthenticationCode = encryptedMac

	errs2 := tree.Metadata.UpdateMasterKeysWithKeyServices(dataKey, svcs)
	if len(errs2) > 0 {
		return nil, fmt.Errorf("failed to update master keys: %v", errs2)
	}

	encrypted, err := store.EmitEncryptedFile(tree)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize encrypted file: %w", err)
	}

	return encrypted, nil
}

// resolveEditor returns the editor command from $EDITOR, $VISUAL, or "vi".
func resolveEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	return "vi"
}

// secureTempDir creates a 0700 temp directory inside runtimeDir.
func (m *Manager) secureTempDir(prefix string) (string, func(), error) {
	baseDir := m.runtimeDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}

	tmpDir, err := os.MkdirTemp(baseDir, prefix)
	if err != nil {
		return "", func() {}, err
	}

	// #nosec G302 -- 0700 required for owner to traverse directory
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, err
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}

// SetKey sets key to value in the store storeName under secretsDir
// without an editor and without writing plaintext anywhere: an existing
// store is decrypted, updated and re-encrypted in memory for its
// current recipients; a missing store is created for
// explicitAgePubKey, or for the discovered identity's recipient when
// that is empty.
func (m *Manager) SetKey(storeName, secretsDir, key, value, explicitAgePubKey string) error {
	if err := validateName(storeName); err != nil {
		return err
	}
	if key == "" {
		return errors.New("secret key name cannot be empty")
	}
	path := filepath.Join(secretsDir, storeName+".enc.yaml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		recipient := explicitAgePubKey
		if recipient == "" {
			identity, err := DiscoverAgeKey()
			if err != nil {
				return err
			}
			if recipient, err = identity.Recipient(); err != nil {
				return err
			}
		}
		content, err := yamlpkg.Marshal(map[string]string{key: value})
		if err != nil {
			return fmt.Errorf("serializing secrets: %w", err)
		}
		return m.CreateFromContent(storeName, secretsDir, recipient, content)
	} else if err != nil {
		return err
	}
	data, err := decryptForUpdate(path)
	if err != nil {
		return err
	}
	data[key] = value
	return reencryptMap(path, data)
}

// DeleteKey removes key from the store storeName under secretsDir. A
// missing store or key is not an error. Deleting the last key removes
// the store file, since sops cannot encrypt an empty map.
func (m *Manager) DeleteKey(storeName, secretsDir, key string) error {
	if err := validateName(storeName); err != nil {
		return err
	}
	path := filepath.Join(secretsDir, storeName+".enc.yaml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	data, err := decryptForUpdate(path)
	if err != nil {
		return err
	}
	if _, ok := data[key]; !ok {
		return nil
	}
	delete(data, key)
	if len(data) == 0 {
		return os.Remove(path)
	}
	return reencryptMap(path, data)
}

func decryptForUpdate(path string) (map[string]string, error) {
	identity, err := DiscoverAgeKey()
	if err != nil {
		return nil, err
	}
	data, err := DecryptSecretsFile(path, identity)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// reencryptMap replaces the store at path with data, encrypted for the
// store's existing recipients.
func reencryptMap(path string, data map[string]string) error {
	content, err := yamlpkg.Marshal(data)
	if err != nil {
		return fmt.Errorf("serializing secrets: %w", err)
	}
	return reencryptInPlace(path, content)
}

// reencryptInPlace encrypts content for the recipients of the store at
// path and atomically replaces it.
func reencryptInPlace(path string, content []byte) error {
	fileBytes, err := os.ReadFile(path) // #nosec G304 -- the store being updated
	if err != nil {
		return fmt.Errorf("failed to read encrypted file: %w", err)
	}
	encTree, err := (&yaml.Store{}).LoadEncryptedFile(fileBytes)
	if err != nil {
		return fmt.Errorf("failed to load encrypted file metadata: %w", err)
	}
	encrypted, err := encryptWithKeyGroups(content, encTree.Metadata.KeyGroups)
	if err != nil {
		return fmt.Errorf("sops re-encryption failed: %w", err)
	}
	if err := fsutil.AtomicWrite(path, encrypted); err != nil {
		return fmt.Errorf("failed to atomically write encrypted file: %w", err)
	}
	return nil
}
