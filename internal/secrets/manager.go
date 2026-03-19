package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/yaml"
	"github.com/jskswamy/aide/internal/config"
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
}

// SecretsFileInfo holds metadata about an encrypted secrets file.
type SecretsFileInfo struct {
	Name         string   // filename (e.g. "personal.enc.yaml")
	Path         string   // full absolute path
	Recipients   []string // age public keys from sops metadata
	ReferencedBy []string // context names that reference this file
}

// NewManager creates a new secrets Manager.
func NewManager(secretsDir, runtimeDir string) *Manager {
	return &Manager{
		secretsDir: secretsDir,
		runtimeDir: runtimeDir,
	}
}

// List scans secretsDir for *.enc.yaml files, extracts sops metadata
// (recipients) without decrypting, and cross-references with config contexts.
func (m *Manager) List(secretsDir string, cfg *config.Config) ([]SecretsFileInfo, error) {
	// Check if directory exists.
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read secrets directory: %w", err)
	}

	// Build reverse map: secretsFile -> []contextName
	contextsByFile := buildContextReverseMap(cfg)

	var results []SecretsFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".enc.yaml") {
			continue
		}

		fullPath := filepath.Join(secretsDir, name)

		// Extract recipients from sops metadata without decrypting.
		recipients := extractRecipients(fullPath)

		// Look up which contexts reference this file.
		referencedBy := contextsByFile[name]
		if referencedBy == nil {
			referencedBy = []string{}
		}
		sort.Strings(referencedBy)

		results = append(results, SecretsFileInfo{
			Name:         name,
			Path:         fullPath,
			Recipients:   recipients,
			ReferencedBy: referencedBy,
		})
	}

	// Sort by name for stable output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results, nil
}

// buildContextReverseMap builds a map from secrets filename to context names.
func buildContextReverseMap(cfg *config.Config) map[string][]string {
	result := make(map[string][]string)
	if cfg == nil {
		return result
	}

	for ctxName, ctx := range cfg.Contexts {
		if ctx.SecretsFile != "" {
			result[ctx.SecretsFile] = append(result[ctx.SecretsFile], ctxName)
		}
	}

	// Handle minimal config with top-level secrets_file.
	if cfg.IsMinimal() && cfg.SecretsFile != "" {
		result[cfg.SecretsFile] = append(result[cfg.SecretsFile], "(default)")
	}

	return result
}

// extractRecipients reads a sops-encrypted file and returns the age recipient
// public keys from its metadata, without decrypting.
func extractRecipients(filePath string) []string {
	recipients, err := ListRecipients(filePath)
	if err != nil {
		return nil
	}
	return recipients
}

// Create creates a new encrypted secrets file by opening $EDITOR.
func (m *Manager) Create(name, secretsDir, agePublicKey string) error {
	if err := validateName(name); err != nil {
		return err
	}

	targetPath := filepath.Join(secretsDir, name+".enc.yaml")
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("secrets/%s.enc.yaml already exists. Use 'aide secrets edit %s' to modify it.", name, name)
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
	editor := resolveEditor()
	cmd := exec.Command(editor, tmpFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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
		return fmt.Errorf("secrets/%s.enc.yaml already exists. Use 'aide secrets edit %s' to modify it.", name, name)
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

	// Write encrypted file.
	if err := os.WriteFile(targetPath, encrypted, 0o600); err != nil {
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
		return fmt.Errorf("invalid secrets name %q. Use alphanumeric, hyphens, underscores only.", name)
	}
	return nil
}

// validateContent checks that content is not empty or comment-only.
func validateContent(content []byte) error {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return fmt.Errorf("No secrets entered. Aborting.")
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
		return fmt.Errorf("No secrets entered. Aborting.")
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

	if err := os.Chmod(tmpDir, 0o700); err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, err
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}
