package secrets

import (
	"io"
	"os/exec"
)

//go:generate mockgen -destination=mocks/mock_editor.go -package=mocks github.com/jskswamy/aide/internal/secrets EditorRunner

// EditorRunner abstracts running an external editor for testability.
type EditorRunner interface {
	// Run launches the editor binary with the given args.
	Run(editor string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// RealEditorRunner launches the actual editor process.
type RealEditorRunner struct{}

// Run executes the editor command.
func (r *RealEditorRunner) Run(editor string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(editor, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
