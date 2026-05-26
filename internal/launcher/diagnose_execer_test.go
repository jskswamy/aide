//go:build !windows

package launcher

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRemapArgvFDs_RewritesParentFDsToChildFDs pins the Greptile P1 fix.
// The sandbox layer embeds the parent's actual memfd fd in argv on the
// assumption that the launcher will use syscall.Exec (which preserves fd
// numbers). Under --diagnose the launcher uses cmd.Start, which dup2s
// ExtraFiles[i] to fd 3+i — without remapping, the child reads a closed
// fd and the sandbox setup fails (Landlock: bad file descriptor; bwrap:
// seccomp filter load failure).
func TestRemapArgvFDs_RewritesParentFDsToChildFDs(t *testing.T) {
	// Acquire two real fds with distinct, non-zero numbers so the test
	// exercises the exact arithmetic (parent N → child 3+i) instead of a
	// coincidental match.
	f0, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer func() { _ = f0.Close() }()
	f1, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer func() { _ = f1.Close() }()

	parent0 := int(f0.Fd())
	parent1 := int(f1.Fd())

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "landlock --policy-fd=N joined",
			in: []string{
				"aide", "__sandbox-apply",
				"--policy-fd=" + itoa(parent0),
				"--", "/usr/local/bin/claude", "--print",
			},
			want: []string{
				"aide", "__sandbox-apply",
				"--policy-fd=3",
				"--", "/usr/local/bin/claude", "--print",
			},
		},
		{
			name: "bwrap --seccomp N split",
			in: []string{
				"bwrap", "--seccomp", itoa(parent0), "--", "/bin/echo", "hi",
			},
			want: []string{
				"bwrap", "--seccomp", "3", "--", "/bin/echo", "hi",
			},
		},
		{
			name: "two extra files at child fds 3 and 4",
			in: []string{
				"bwrap", "--seccomp", itoa(parent1),
				"--policy-fd=" + itoa(parent0),
				"--", "/bin/echo",
			},
			want: []string{
				"bwrap", "--seccomp", "4",
				"--policy-fd=3",
				"--", "/bin/echo",
			},
		},
		{
			name: "fd number that happens to appear elsewhere is not touched",
			in: []string{
				"aide", "--unrelated", itoa(parent0), "--policy-fd=" + itoa(parent0),
			},
			want: []string{
				"aide", "--unrelated", itoa(parent0), "--policy-fd=3",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var files []*os.File
			if strings.Contains(tc.name, "two extra files") {
				files = []*os.File{f0, f1}
			} else {
				files = []*os.File{f0}
			}
			got := remapArgvFDs(tc.in, files)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("remapArgvFDs:\n got  %v\n want %v", got, tc.want)
			}
			// Defensive-copy contract: mutating result must not affect input.
			if len(got) > 0 {
				got[0] = "MUTATED"
				if tc.in[0] == "MUTATED" {
					t.Error("remapArgvFDs returned a slice aliasing the input")
				}
			}
		})
	}
}

func TestRemapArgvFDs_NoExtraFilesIsNoOp(t *testing.T) {
	in := []string{"aide", "--policy-fd=42"}
	got := remapArgvFDs(in, nil)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("remapArgvFDs with no extraFiles must be a no-op; got %v", got)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestDiagnoseExecer_CapturesExitCodeAndStderr(t *testing.T) {
	e := &DiagnoseExecer{StderrLineLimit: 100, StderrByteLimit: 4096}
	res, err := e.Run("/bin/sh", []string{"sh", "-c", "echo hello-stderr 1>&2; exit 7"}, nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if !strings.Contains(res.StderrTail, "hello-stderr") {
		t.Errorf("StderrTail does not contain captured output: %q", res.StderrTail)
	}
	if res.Runtime <= 0 || res.Runtime > 5*time.Second {
		t.Errorf("Runtime out of range: %v", res.Runtime)
	}
	if res.Signal != "" {
		t.Errorf("Signal should be empty for clean non-zero exit, got %q", res.Signal)
	}
}

func TestDiagnoseExecer_TruncatesAtByteLimit(t *testing.T) {
	e := &DiagnoseExecer{StderrLineLimit: 10000, StderrByteLimit: 50}
	res, err := e.Run("/bin/sh", []string{"sh", "-c", "yes XXXXXXXX | head -c 4000 1>&2"}, nil, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if int64(len(res.StderrTail)) > 200 { // small slack: truncation marker is short
		t.Errorf("StderrTail not truncated: len=%d", len(res.StderrTail))
	}
	if res.StderrTruncatedBytes <= 0 {
		t.Errorf("StderrTruncatedBytes should be >0, got %d", res.StderrTruncatedBytes)
	}
}
