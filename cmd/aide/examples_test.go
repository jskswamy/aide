// cmd/aide/examples_test.go
package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// exampleLine matches lines that start with `aide ` (after optional
// leading whitespace). The captured group is everything after `aide `.
var exampleLine = regexp.MustCompile(`^\s*aide\s+(.+)$`)

func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, child := range c.Commands() {
		walkCommands(child, fn)
	}
}

// buildTestRoot returns a cobra root command with every subcommand
// registered. It reuses production registerCommands() so the test surface
// is exactly main()'s subcommand tree.
func buildTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "aide"}
	registerCommands(root)
	return root
}

func TestHelpExamplesParse(t *testing.T) {
	root := buildTestRoot()

	walkCommands(root, func(c *cobra.Command) {
		if c.Long == "" {
			return
		}
		for raw := range strings.SplitSeq(c.Long, "\n") {
			m := exampleLine.FindStringSubmatch(raw)
			if m == nil {
				continue
			}
			// Strip trailing # comment.
			line := m[1]
			if idx := strings.Index(line, "#"); idx >= 0 {
				line = strings.TrimSpace(line[:idx])
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}

			t.Run(line, func(t *testing.T) {
				// Build a fresh root per example to avoid flag-state bleed.
				freshRoot := buildTestRoot()
				freshRoot.SetArgs(append(fields, "--help"))
				var buf bytes.Buffer
				freshRoot.SetOut(&buf)
				freshRoot.SetErr(&buf)
				// --help short-circuits the handler, so this only validates
				// the command path and flag names are recognized.
				if err := freshRoot.Execute(); err != nil {
					t.Errorf("example %q failed to parse: %v\noutput: %s", line, err, buf.String())
				}
			})
		}
	})
}
