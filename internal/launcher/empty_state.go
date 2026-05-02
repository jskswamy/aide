package launcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

// ErrEmptyStateCancelled is returned when the user picks [c] at the
// empty-state prompt.
var ErrEmptyStateCancelled = errors.New("aide: cancelled at empty-state prompt")

// ErrEmptyStateActionRanReloadNeeded signals that a [1]/[2] action ran
// successfully and the caller should reload config and re-resolve
// context before continuing the launch. The caller (launcher.Launch)
// handles this transparently; users never see this error.
var ErrEmptyStateActionRanReloadNeeded = errors.New("empty-state action completed; caller should reload config")

// EmptyStateActions is the contract the launcher needs from the cmd
// layer to dispatch [1] / [2] choices. The cmd layer implements this
// using the same code paths as the standalone `bind` / `create`
// commands so behavior matches across surfaces.
type EmptyStateActions interface {
	// Bind attaches cwd to an existing context. The provided name is
	// the user's pick from the empty-state picker; an empty name means
	// the action should run its own picker (e.g. when no context list
	// was shown).
	Bind(name string) error
	// Create runs the create wizard. The provided name pre-fills the
	// wizard's first question; an empty string means "ask".
	Create(name string) error
}

// handleEmptyState runs the interactive prompt (in TTY mode) or returns
// a hard error (in non-TTY mode) when the launcher cannot resolve a
// context for the current folder.
//
// Returns:
//   - In non-TTY mode: error with copy-pasteable next-command hints.
//   - Choice [1]/[2]: invokes actions; returns ErrEmptyStateActionRanReloadNeeded
//     so the caller knows to reload config and re-resolve.
//   - Choice [3]: returns a *ResolvedContext WITHOUT persisting anything.
//   - Choice [c]: returns ErrEmptyStateCancelled.
func handleEmptyState(
	cfg *config.Config,
	in io.Reader,
	out io.Writer,
	tty bool,
	actions EmptyStateActions,
) (*aidectx.ResolvedContext, error) {
	if !tty {
		return nil, fmt.Errorf(
			"aide: no context matches this folder, and no default_context is configured.\n\n" +
				"To proceed, run one of:\n" +
				"  aide context bind <name>            # attach this folder to existing context\n" +
				"  aide context create [name]          # create a new context for this folder\n" +
				"  aide use <name> -- <agent-args>     # launch once without persisting\n" +
				"  aide context set-default <name>     # use a fallback for unmatched folders",
		)
	}

	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "aide: no context matches this folder.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "What do you want to do?")
	fmt.Fprintln(out, "  [1] Bind this folder to an existing context")
	fmt.Fprintln(out, "  [2] Create a new context for this folder")
	fmt.Fprintln(out, "  [3] Launch once with an existing context (don't save)")
	fmt.Fprintln(out, "  [c] Cancel")
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Choose [1]: ")

	raw, _ := reader.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(raw))
	if choice == "" {
		choice = "1"
	}

	switch choice {
	case "1":
		picked, err := pickContextForLaunchOnce(cfg, reader, out)
		if err != nil {
			return nil, err
		}
		if err := actions.Bind(picked); err != nil {
			return nil, err
		}
		return nil, ErrEmptyStateActionRanReloadNeeded
	case "2":
		if err := actions.Create(""); err != nil {
			return nil, err
		}
		return nil, ErrEmptyStateActionRanReloadNeeded
	case "3":
		picked, err := pickContextForLaunchOnce(cfg, reader, out)
		if err != nil {
			return nil, err
		}
		ctx := cfg.Contexts[picked]
		return &aidectx.ResolvedContext{
			Name:        picked,
			MatchReason: "empty-state launch-once",
			Context:     ctx,
		}, nil
	case "c", "cancel":
		return nil, ErrEmptyStateCancelled
	default:
		return nil, fmt.Errorf("invalid choice: %q", choice)
	}
}

// pickContextForLaunchOnce shows a numbered menu of existing contexts
// and returns the chosen name.
func pickContextForLaunchOnce(cfg *config.Config, reader *bufio.Reader, out io.Writer) (string, error) {
	if len(cfg.Contexts) == 0 {
		return "", fmt.Errorf("no contexts configured. Run: aide context create <name>")
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Existing contexts:")
	for i, n := range names {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, n)
	}
	fmt.Fprint(out, "Choose [1]: ")
	raw, _ := reader.ReadString('\n')
	input := strings.TrimSpace(raw)
	choice := 1
	if input != "" {
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(names) {
			return "", fmt.Errorf("invalid selection: %q", input)
		}
		choice = n
	}
	return names[choice-1], nil
}
