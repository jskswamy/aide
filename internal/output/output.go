// Package output implements aide's --format human|json dispatch: flag
// registration (with an AIDE_FORMAT environment-variable default),
// format validation, and the human-vs-JSON emit/error-print switch
// every read/query command shares.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Format is a validated output format.
type Format string

const (
	Human Format = "human"
	JSON  Format = "json"
)

// Parse validates a raw --format value. Only "human" and "json" are
// accepted — explain's "agent" value is handled entirely inside
// explain.go and never passes through this package.
func Parse(raw string) (Format, error) {
	switch Format(raw) {
	case Human, JSON:
		return Format(raw), nil
	default:
		return "", fmt.Errorf("unknown --format %q (want human or json)", raw)
	}
}

// DefaultFromEnv returns $AIDE_FORMAT if set, otherwise fallback. Used
// both by RegisterFlag (for the persistent --format flag's default)
// and by explain.go (whose local --format flag also accepts "agent").
func DefaultFromEnv(fallback string) string {
	if v, ok := os.LookupEnv("AIDE_FORMAT"); ok {
		return v
	}
	return fallback
}

// RegisterFlag registers the persistent --format flag on cmd, so every
// descendant command inherits it. Call once on rootCmd in production;
// in tests, call on whatever command tree the test constructs, before
// cmd.Execute() — see the Global Constraints test-harness note.
func RegisterFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String("format", DefaultFromEnv(string(Human)),
		"Output format: human or json (default: $AIDE_FORMAT, or human)")
}

// FromCmd resolves cmd's effective format. It checks cmd's own merged
// flag set first (the normal case inside a RunE, where cobra has
// already merged inherited persistent flags), then falls back to
// cmd.PersistentFlags() directly (for callers like main.go reading
// rootCmd's value after Execute() returns, when rootCmd itself never
// ran its own RunE). If the flag isn't registered at all on this
// command tree, returns Human — this is what makes every existing
// cmd/aide test keep passing without modification.
func FromCmd(cmd *cobra.Command) (Format, error) {
	raw, err := cmd.Flags().GetString("format")
	if err != nil {
		raw, err = cmd.PersistentFlags().GetString("format")
		if err != nil {
			return Human, nil
		}
	}
	return Parse(raw)
}

// Emit writes data as indented JSON when format is JSON, otherwise
// calls humanFn to run the command's existing human-mode print logic.
func Emit(w io.Writer, format Format, data any, humanFn func(io.Writer) error) error {
	if format == JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	return humanFn(w)
}

// PrintError writes err as {"error": "..."} when format is JSON,
// otherwise as plain text matching cobra's default "Error: ..." shape.
func PrintError(w io.Writer, format Format, err error) {
	if format == JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"error": err.Error()})
		return
	}
	fmt.Fprintln(w, "Error:", err.Error())
}
