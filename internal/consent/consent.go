package consent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jskswamy/aide/internal/approvalstore"
)

// Status is the consent state for a (project, capability, variants,
// evidence) tuple.
type Status int

// Consent status values returned by Store.Check.
const (
	// NotGranted indicates no matching grant is stored for the tuple.
	NotGranted Status = iota
	// Granted indicates a matching grant exists.
	Granted
)

// ErrInvalidGrantField is returned by Grant when a field contains a
// character that would corrupt the stored body. The wrapped detail
// names which field and why.
var ErrInvalidGrantField = errors.New("consent: invalid grant field")

// validateGrantFields ensures no field contains characters that would
// break the line-oriented body format. ProjectRoot, Capability, and
// Summary must not contain newlines; Variants must not contain newlines
// or commas (commas are the variant-list separator).
func validateGrantFields(g Grant) error {
	if strings.ContainsAny(g.ProjectRoot, "\r\n") {
		return fmt.Errorf("%w: ProjectRoot contains newline", ErrInvalidGrantField)
	}
	if strings.ContainsAny(g.Capability, "\r\n") {
		return fmt.Errorf("%w: Capability contains newline", ErrInvalidGrantField)
	}
	if strings.ContainsAny(g.Summary, "\r\n") {
		return fmt.Errorf("%w: Summary contains newline", ErrInvalidGrantField)
	}
	for i, v := range g.Variants {
		if strings.ContainsAny(v, "\r\n,") {
			return fmt.Errorf("%w: Variants[%d] contains newline or comma", ErrInvalidGrantField, i)
		}
	}
	return nil
}

// Grant records one user approval.
type Grant struct {
	ProjectRoot string
	Capability  string
	Variants    []string
	Evidence    Evidence
	Summary     string
	ConfirmedAt time.Time
}

// Store persists grants under XDG_DATA_HOME/aide/consent/.
type Store struct {
	set *approvalstore.Store
}

// NewStore creates a consent store rooted at baseDir. baseDir should
// be the XDG-shared aide root; the store nests into consent/.
func NewStore(baseDir string) *Store {
	return &Store{set: approvalstore.NewStore(filepath.Join(baseDir, "consent"))}
}

// DefaultStore returns a Store under approvalstore.DefaultRoot().
func DefaultStore() *Store {
	return NewStore(approvalstore.DefaultRoot())
}

// Hash computes the content-addressed key for a grant. The
// variants slice is sorted internally so ordering does not affect the
// hash. The encoding is length-prefixed to be injective: scope fields
// that differ only in where their separators fall cannot collide.
func Hash(projectRoot, capability string, variants []string, evidenceDigest string) string {
	sorted := append([]string(nil), variants...)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte("consent-v1\n"))
	writeLenPrefixed(h, projectRoot)
	writeLenPrefixed(h, capability)
	writeLenPrefixed(h, strings.Join(sorted, ","))
	writeLenPrefixed(h, evidenceDigest)
	return hex.EncodeToString(h.Sum(nil))
}

// Check returns Granted when a record exists matching the exact
// (project, capability, evidence.Variants, evidence.Digest()) tuple.
func (s *Store) Check(projectRoot, capability string, evidence Evidence) Status {
	key := Hash(projectRoot, capability, evidence.Variants, evidence.Digest())
	if s.set.Has(key) {
		return Granted
	}
	return NotGranted
}

// Grant records an approval. ConfirmedAt is set to time.Now().UTC() if zero.
// Fields that would break the stored body format (newlines in text fields,
// newlines or commas in variants) are rejected with ErrInvalidGrantField.
func (s *Store) Grant(g Grant) error {
	if err := validateGrantFields(g); err != nil {
		return err
	}
	if g.ConfirmedAt.IsZero() {
		g.ConfirmedAt = time.Now().UTC()
	}
	key := Hash(g.ProjectRoot, g.Capability, g.Variants, g.Evidence.Digest())
	body := fmt.Sprintf(
		"project: %s\ncapability: %s\nvariants: %s\nevidence_digest: %s\nevidence_summary: %s\nconfirmed_at: %s\n",
		g.ProjectRoot,
		g.Capability,
		strings.Join(g.Variants, ","),
		g.Evidence.Digest(),
		g.Summary,
		g.ConfirmedAt.Format(time.RFC3339),
	)
	return s.set.Add(key, []byte(body))
}

// Revoke removes every record whose stored body matches projectRoot
// and capability, regardless of variants or evidence digest.
func (s *Store) Revoke(projectRoot, capability string) error {
	records, err := s.set.List()
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("project: %s\ncapability: %s\n", projectRoot, capability)
	for _, r := range records {
		if strings.HasPrefix(string(r.Body), prefix) {
			if err := s.set.Remove(r.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

// List returns all grants for projectRoot sorted by capability then
// confirmed time.
func (s *Store) List(projectRoot string) ([]Grant, error) {
	records, err := s.set.List()
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("project: %s\n", projectRoot)
	out := make([]Grant, 0)
	for _, r := range records {
		if !strings.HasPrefix(string(r.Body), prefix) {
			continue
		}
		g, ok := parseGrantBody(r.Body)
		if !ok {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Capability != out[j].Capability {
			return out[i].Capability < out[j].Capability
		}
		return out[i].ConfirmedAt.Before(out[j].ConfirmedAt)
	})
	return out, nil
}

func parseGrantBody(body []byte) (Grant, bool) {
	g := Grant{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "project":
			g.ProjectRoot = parts[1]
		case "capability":
			g.Capability = parts[1]
		case "variants":
			if parts[1] != "" {
				g.Variants = strings.Split(parts[1], ",")
			}
		case "evidence_summary":
			g.Summary = parts[1]
		case "confirmed_at":
			if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
				g.ConfirmedAt = t
			}
		}
	}
	if g.ProjectRoot == "" || g.Capability == "" {
		return Grant{}, false
	}
	return g, true
}
