package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Writer persists rendered reports to disk. On any I/O failure it falls
// back to writing the body to a fallback writer (typically os.Stderr) so
// the diagnostic is never lost.
type Writer struct {
	CacheDir string                                    // e.g. ~/.cache/aide/diagnose
	Now      func() time.Time                          // override for tests
	MkdirAll func(path string, perm os.FileMode) error // override for tests
}

func (w *Writer) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now().UTC()
}

func (w *Writer) mkdirAll(p string, m os.FileMode) error {
	if w.MkdirAll != nil {
		return w.MkdirAll(p, m)
	}
	return os.MkdirAll(p, m)
}

// Write persists body. Returns the file path on success, or "" with the
// report echoed to fallback (typically os.Stderr) on failure plus the
// underlying error so the caller can decide how to surface it.
func (w *Writer) Write(body string, fallback io.Writer) (string, error) {
	idSeed := body
	if len(body) > 256 {
		idSeed = body[:256]
	}
	path := w.path(idSeed)
	if err := w.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.fallback(body, fallback, err)
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		w.fallback(body, fallback, err)
		return "", err
	}
	return path, nil
}

// path returns the full file path for a given seed. Format:
//
//	<cacheDir>/<RFC3339-timestamp>-<short-hash>.md
//
// where short-hash is the first 8 hex chars of SHA-256(seed) so concurrent
// runs from different cwds don't collide.
func (w *Writer) path(seed string) string {
	ts := w.now().Format("2006-01-02T15-04-05Z")
	h := sha256.Sum256([]byte(seed))
	short := hex.EncodeToString(h[:4]) // 8 chars
	name := fmt.Sprintf("%s-%s.md", ts, short)
	return filepath.Join(w.CacheDir, name)
}

func (w *Writer) fallback(body string, fallback io.Writer, err error) {
	fmt.Fprintf(fallback, "warning: could not write diagnose report (%v); dumping inline:\n", err)
	_, _ = io.WriteString(fallback, body)
	if !strings.HasSuffix(body, "\n") {
		_, _ = io.WriteString(fallback, "\n")
	}
}
