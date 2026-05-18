package provision

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
}

func (f fakeRunner) Run(_ context.Context, _ map[string]string, _ string, _ ...string) (string, string, int, error) {
	return f.stdout, f.stderr, f.code, f.err
}

func TestRunCLI(t *testing.T) {
	t.Run("success returns nil", func(t *testing.T) {
		if err := RunCLI(context.Background(), fakeRunner{}, nil, "agent op x", "agent", []string{"op", "x"}); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("runner error is wrapped", func(t *testing.T) {
		sentinel := errors.New("spawn failed")
		err := RunCLI(context.Background(), fakeRunner{err: sentinel}, nil, "agent op x", "agent", []string{"op", "x"})
		if err == nil {
			t.Fatalf("want error")
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("want wrap of sentinel, got %v", err)
		}
		if !strings.HasPrefix(err.Error(), "agent op x: ") {
			t.Errorf("want opDesc prefix, got %q", err.Error())
		}
	})

	t.Run("non-zero exit becomes error", func(t *testing.T) {
		err := RunCLI(context.Background(), fakeRunner{code: 2, stderr: "boom"}, nil, "agent op x", "agent", []string{"op", "x"})
		if err == nil || !strings.Contains(err.Error(), "exit 2") || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("want exit 2/boom error, got %v", err)
		}
	})

	t.Run("tolerated stderr becomes nil", func(t *testing.T) {
		for _, tok := range DefaultTolerateStderr {
			t.Run(tok, func(t *testing.T) {
				err := RunCLI(context.Background(), fakeRunner{code: 1, stderr: "thing " + tok}, nil, "agent op x", "agent", []string{"op", "x"}, DefaultTolerateStderr...)
				if err != nil {
					t.Errorf("want nil for tolerated %q, got %v", tok, err)
				}
			})
		}
	})

	t.Run("non-tolerated stderr stays error", func(t *testing.T) {
		err := RunCLI(context.Background(), fakeRunner{code: 1, stderr: "other failure"}, nil, "agent op x", "agent", []string{"op", "x"}, DefaultTolerateStderr...)
		if err == nil {
			t.Fatalf("want error for non-tolerated stderr")
		}
	})

	t.Run("empty tolerate token never matches", func(t *testing.T) {
		err := RunCLI(context.Background(), fakeRunner{code: 1, stderr: ""}, nil, "agent op x", "agent", []string{"op", "x"}, "")
		if err == nil {
			t.Fatalf("want error for empty stderr with empty tolerate token")
		}
	})
}
