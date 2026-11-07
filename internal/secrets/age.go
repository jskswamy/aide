// Package secrets handles age-based encryption and decryption of secret files.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
)

// AgeRecipient represents a discovered age public key recipient, its source,
// and an optional descriptive label from preceding comment lines.
type AgeRecipient struct {
	PublicKey string
	Source    string
	Label     string
}

var recipientCommentRe = regexp.MustCompile(`(?i)^#\s*(?:public key|recipient):\s*(age1\S+)`)

func parseRecipientsFromReader(r io.Reader, source string) ([]AgeRecipient, []*age.X25519Identity, error) {
	var recipients []AgeRecipient
	var identities []*age.X25519Identity
	var pendingLabel string
	var pendingPubKey string

	flushPending := func() {
		if pendingPubKey != "" {
			recipients = append(recipients, AgeRecipient{
				PublicKey: pendingPubKey,
				Source:    source,
				Label:     pendingLabel,
			})
			pendingPubKey = ""
			pendingLabel = ""
		}
	}

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			flushPending()
			pendingLabel = ""
			continue
		}
		if strings.HasPrefix(line, "#") {
			if m := recipientCommentRe.FindStringSubmatch(line); len(m) == 2 {
				flushPending()
				pendingPubKey = m[1]
			} else {
				flushPending()
				comment := strings.TrimSpace(strings.TrimPrefix(line, "#"))
				lower := strings.ToLower(comment)
				if comment != "" && !strings.HasPrefix(lower, "public key:") && !strings.HasPrefix(lower, "recipient:") {
					pendingLabel = comment
				}
			}
			continue
		}
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			x, err := age.ParseX25519Identity(line)
			if err != nil {
				return nil, nil, fmt.Errorf("parsing age identity: %w", err)
			}
			identities = append(identities, x)
			recipients = append(recipients, AgeRecipient{
				PublicKey: x.Recipient().String(),
				Source:    source,
				Label:     pendingLabel,
			})
			pendingPubKey = ""
			pendingLabel = ""
			continue
		}
		if strings.HasPrefix(line, "AGE-PLUGIN-") {
			flushPending()
			pendingLabel = ""
			continue
		}
		flushPending()
		pendingLabel = ""
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading age identity: %w", err)
	}
	flushPending()
	return recipients, identities, nil
}

func hasSecretKeyPrefixLine(s string) bool {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "AGE-SECRET-KEY-") {
			return true
		}
	}
	return false
}

// AgeKeySource describes where an age key was found.
type AgeKeySource int

// AgeKeySource values identify where an age key was discovered.
const (
	SourceYubiKey     AgeKeySource = iota
	SourceEnvKey                   // SOPS_AGE_KEY
	SourceEnvKeyFile               // SOPS_AGE_KEY_FILE
	SourceDefaultFile              // XDG default path
)

// AgeIdentity holds a discovered age identity for sops decryption.
type AgeIdentity struct {
	Source AgeKeySource
	// KeyData contains the raw key material for SourceEnvKey,
	// or the file path for SourceEnvKeyFile / SourceDefaultFile.
	// For SourceYubiKey this is empty (plugin handles it).
	KeyData string
}

// Recipient returns the public age1... recipient of a software
// identity, for encrypting a new store to it. A YubiKey identity's
// recipient is not derivable here; pass one explicitly (--age-key).
func (id *AgeIdentity) Recipient() (string, error) {
	var r io.Reader
	var source string
	switch id.Source {
	case SourceYubiKey:
		return "", fmt.Errorf("cannot derive an age recipient from a YubiKey identity; pass --age-key <age1...>")
	case SourceEnvKey:
		r = strings.NewReader(id.KeyData)
		source = "env:SOPS_AGE_KEY"
	case SourceEnvKeyFile, SourceDefaultFile:
		f, err := os.Open(id.KeyData) // #nosec G304 -- the discovered age key file
		if err != nil {
			return "", fmt.Errorf("reading age key file: %w", err)
		}
		defer func() { _ = f.Close() }()
		r = f
		source = id.KeyData
	default:
		return "", fmt.Errorf("unknown age identity source: %d", id.Source)
	}
	recs, _, err := parseRecipientsFromReader(r, source)
	if err != nil {
		return "", err
	}
	if len(recs) > 0 {
		return recs[0].PublicKey, nil
	}
	return "", fmt.Errorf("no software age identity found; pass --age-key <age1...>")
}

// DiscoverAgeKey searches for an age identity in priority order.
// Returns the first identity found, or an error with guidance
// if no identity is available.
func DiscoverAgeKey() (*AgeIdentity, error) {
	// 1. YubiKey: detect age-plugin-yubikey on PATH.
	if _, err := exec.LookPath("age-plugin-yubikey"); err == nil {
		return &AgeIdentity{Source: SourceYubiKey}, nil
	}

	// 2. SOPS_AGE_KEY environment variable.
	if val := os.Getenv("SOPS_AGE_KEY"); val != "" {
		if recs, ids, err := parseRecipientsFromReader(strings.NewReader(val), "env:SOPS_AGE_KEY"); (err == nil && (len(ids) > 0 || len(recs) > 0)) || hasSecretKeyPrefixLine(val) {
			return &AgeIdentity{Source: SourceEnvKey, KeyData: val}, nil
		}
		// Invalid prefix — skip silently, fall through to next source.
	}

	// 3. SOPS_AGE_KEY_FILE environment variable.
	if val := os.Getenv("SOPS_AGE_KEY_FILE"); val != "" {
		if fileReadable(val) {
			return &AgeIdentity{Source: SourceEnvKeyFile, KeyData: val}, nil
		}
		// File missing or unreadable — skip silently.
	}

	// 4. Default key path: OS-canonical sops location, with XDG fallback on macOS.
	for _, p := range defaultKeyPaths() {
		if fileReadable(p) {
			return &AgeIdentity{Source: SourceDefaultFile, KeyData: p}, nil
		}
	}

	return nil, fmt.Errorf(`no age identity found. aide needs an age key to decrypt secrets.

Options:
  1. Plug in a YubiKey with age-plugin-yubikey installed
  2. Set SOPS_AGE_KEY env var (for CI/Docker)
  3. Set SOPS_AGE_KEY_FILE to point to your key file
  4. Run: age-keygen -o %s

Run 'aide init' or 'aide secrets create <name>' to generate or configure a key`, defaultKeyPath())
}

func parseRecipientsFromFile(path string) ([]AgeRecipient, error) {
	f, err := os.Open(path) // #nosec G304 -- user-configured or default age key file
	if err != nil {
		return nil, fmt.Errorf("reading age key file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	recs, _, err := parseRecipientsFromReader(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading age key file %s: %w", path, err)
	}
	return recs, nil
}

// DiscoverRecipients collects all age1... recipients from SOPS_AGE_KEY,
// SOPS_AGE_KEY_FILE, and defaultKeyPaths(), deduplicating by PublicKey
// while preserving first-seen order.
func DiscoverRecipients() ([]AgeRecipient, error) {
	var all []AgeRecipient
	seen := make(map[string]bool)
	addAll := func(recs []AgeRecipient) {
		for _, r := range recs {
			if !seen[r.PublicKey] {
				seen[r.PublicKey] = true
				all = append(all, r)
			}
		}
	}

	if val := os.Getenv("SOPS_AGE_KEY"); strings.TrimSpace(val) != "" {
		recs, _, err := parseRecipientsFromReader(strings.NewReader(val), "env:SOPS_AGE_KEY")
		if err != nil {
			return nil, fmt.Errorf("SOPS_AGE_KEY: %w", err)
		}
		addAll(recs)
	}

	if val := os.Getenv("SOPS_AGE_KEY_FILE"); strings.TrimSpace(val) != "" {
		if !fileReadable(val) {
			return nil, fmt.Errorf("reading SOPS_AGE_KEY_FILE=%s: file is not readable", val)
		}
		recs, err := parseRecipientsFromFile(val)
		if err != nil {
			return nil, err
		}
		addAll(recs)
	}

	for _, p := range defaultKeyPaths() {
		if !fileReadable(p) {
			continue
		}
		recs, err := parseRecipientsFromFile(p)
		if err != nil {
			return nil, err
		}
		addAll(recs)
	}

	return all, nil
}

// GenerateDefaultAgeKey generates a new X25519 age keypair in-process and
// writes or appends it to targetPath (or defaultKeyPath() when targetPath == "").
func GenerateDefaultAgeKey(targetPath string) (AgeRecipient, error) {
	if targetPath == "" {
		targetPath = defaultKeyPath()
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return AgeRecipient{}, fmt.Errorf("generating age identity: %w", err)
	}
	block := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n",
		time.Now().UTC().Format(time.RFC3339),
		id.Recipient().String(),
		id.String(),
	)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return AgeRecipient{}, fmt.Errorf("creating age key directory: %w", err)
	}
	existing, err := os.ReadFile(targetPath) // #nosec G304 -- user-configured or default age key path
	if err != nil && !os.IsNotExist(err) {
		return AgeRecipient{}, fmt.Errorf("reading existing age key file: %w", err)
	}
	var content []byte
	if len(strings.TrimSpace(string(existing))) > 0 {
		trimmed := strings.TrimRight(string(existing), "\n")
		content = []byte(trimmed + "\n\n" + block)
	} else {
		content = []byte(block)
	}
	if err := os.WriteFile(targetPath, content, 0o600); err != nil {
		return AgeRecipient{}, fmt.Errorf("writing age key file: %w", err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		return AgeRecipient{}, fmt.Errorf("chmod age key file: %w", err)
	}
	return AgeRecipient{
		PublicKey: id.Recipient().String(),
		Source:    targetPath,
		Label:     "generated",
	}, nil
}

// ValidateRecipient checks that pubKey is a valid standard age1... X25519
// recipient or a plugin Bech32 recipient (such as age1yubikey1...).
func ValidateRecipient(pubKey string) error {
	pubKey = strings.TrimSpace(pubKey)
	if pubKey == "" {
		return fmt.Errorf("age public key cannot be empty")
	}
	if !strings.HasPrefix(pubKey, "age1") {
		return fmt.Errorf("invalid age public key %q: must start with age1", pubKey)
	}
	if strings.ContainsAny(pubKey, " \t\r\n") {
		return fmt.Errorf("invalid age public key %q: must not contain whitespace", pubKey)
	}
	if _, err := age.ParseX25519Recipient(pubKey); err == nil {
		return nil
	}
	// Allow plugin Bech32 recipients such as age1yubikey1... where the HRP
	// is "age1<plugin>" followed by Bech32 separator '1' and data payload.
	if lastOne := strings.LastIndexByte(pubKey, '1'); lastOne > 4 && len(pubKey[lastOne+1:]) >= 6 {
		return nil
	}
	return fmt.Errorf("invalid age public key %q", pubKey)
}

func sourceAndLabel(c AgeRecipient) string {
	if c.Label != "" {
		return c.Source + ", " + c.Label
	}
	return c.Source
}

func readPromptLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptCustomRecipient(in *bufio.Reader, out io.Writer) (string, error) {
	_, _ = fmt.Fprint(out, "Age public key (age1...): ")
	keyLine, err := readPromptLine(in)
	if err != nil {
		return "", err
	}
	if err := ValidateRecipient(keyLine); err != nil {
		return "", err
	}
	return keyLine, nil
}

// ResolveRecipient resolves an age1... public key from an explicit flag,
// discovered keys (auto-selecting 1 or prompting on TTY for 2+), or interactive
// key generation when 0 keys exist on a TTY.
func ResolveRecipient(explicitPubKey string, in *bufio.Reader, out io.Writer, isTTY bool) (string, error) {
	if out == nil {
		out = io.Discard
	}
	if trimmed := strings.TrimSpace(explicitPubKey); trimmed != "" {
		if err := ValidateRecipient(trimmed); err != nil {
			return "", err
		}
		return trimmed, nil
	}

	candidates, err := DiscoverRecipients()
	if err != nil {
		return "", err
	}

	if len(candidates) == 1 {
		_, _ = fmt.Fprintf(out, "Using age key: %s (%s)\n", candidates[0].PublicKey, candidates[0].Source)
		return candidates[0].PublicKey, nil
	}

	if len(candidates) > 1 {
		if !isTTY || in == nil {
			_, _ = fmt.Fprintf(out, "Using age key: %s (%s)\n", candidates[0].PublicKey, candidates[0].Source)
			return candidates[0].PublicKey, nil
		}
		_, _ = fmt.Fprintln(out, "Multiple age keys found:")
		for i, c := range candidates {
			_, _ = fmt.Fprintf(out, "  [%d] %s (%s)\n", i+1, c.PublicKey, sourceAndLabel(c))
		}
		_, _ = fmt.Fprintf(out, "  [%d] Enter a different age public key (age1...)\n", len(candidates)+1)
		_, _ = fmt.Fprint(out, "Select [1]: ")
		line, err := readPromptLine(in)
		if err != nil {
			return "", err
		}
		if line == "" {
			line = "1"
		}
		choice, convErr := strconv.Atoi(line)
		if convErr != nil || choice < 1 || choice > len(candidates)+1 {
			return "", fmt.Errorf("invalid selection: %q", line)
		}
		if choice <= len(candidates) {
			return candidates[choice-1].PublicKey, nil
		}
		return promptCustomRecipient(in, out)
	}

	// len(candidates) == 0
	if !isTTY || in == nil {
		if _, err := DiscoverAgeKey(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no age encryption key found; pass --age-key <age1...>")
	}

	defPath := defaultKeyPath()
	_, _ = fmt.Fprintln(out, "No age encryption key found.")
	_, _ = fmt.Fprintf(out, "  [1] Generate a new age key at %s (default)\n", defPath)
	_, _ = fmt.Fprintln(out, "  [2] Enter an existing age public key (age1...)")
	_, _ = fmt.Fprint(out, "Select [1]: ")
	line, err := readPromptLine(in)
	if err != nil {
		return "", err
	}
	if line == "" {
		line = "1"
	}
	switch line {
	case "1":
		rec, err := GenerateDefaultAgeKey(defPath)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(out, "✓ Generated age key at %s (public key: %s)\n", rec.Source, rec.PublicKey)
		return rec.PublicKey, nil
	case "2":
		return promptCustomRecipient(in, out)
	default:
		return "", fmt.Errorf("invalid selection: %q", line)
	}
}

// defaultKeyPath returns the OS-canonical sops age key location for messages.
// On macOS this is ~/Library/Application Support/sops/age/keys.txt; on Linux
// it honors $XDG_CONFIG_HOME (or ~/.config). Matches sops upstream behavior.
func defaultKeyPath() string {
	paths := defaultKeyPaths()
	if len(paths) == 0 {
		return filepath.Join("~", ".config", "sops", "age", "keys.txt")
	}
	return paths[0]
}

// defaultKeyPaths returns candidate sops age key locations in priority order.
// The first entry is the OS-canonical path (per Go's os.UserConfigDir, matching
// sops). On macOS, the XDG-style ~/.config path is appended as a secondary
// candidate so users with cross-platform setups still resolve their key.
func defaultKeyPaths() []string {
	var paths []string

	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "sops", "age", "keys.txt"))
	}

	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			xdg := filepath.Join(home, ".config", "sops", "age", "keys.txt")
			if len(paths) == 0 || paths[0] != xdg {
				paths = append(paths, xdg)
			}
		}
	}

	return paths
}

// fileReadable returns true if the path exists and is a regular file.
func fileReadable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
