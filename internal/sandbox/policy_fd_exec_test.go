//go:build linux

package sandbox

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// dispatchPolicyFDHelper implements two subprocess helpers used by
// TestPolicyFD_SurvivesSyscallExec to reproduce the launcher's real exec
// flow. It is invoked from TestMain because the modes have to take over the
// test binary before any test runs.
//
// Modes:
//
//	policyFD_exec   — replicates SyscallExecer.Exec semantics: build a
//	                  Cmd with ExtraFiles holding a memfd policy, then
//	                  call syscall.Exec (NOT cmd.Start()). Because
//	                  syscall.Exec is a libc call that does not consult
//	                  the Go *exec.Cmd struct, ExtraFiles is silently
//	                  dropped — exactly what production does today.
//	policyFD_reader — runs in the exec'd-into image. Reads the fd that
//	                  was supposed to carry the policy and reports OK or
//	                  the read error to stdout so the parent test can
//	                  assert on it.
func dispatchPolicyFDHelper() {
	switch os.Getenv("AIDE_TEST_POLICY_FD_MODE") {
	case "exec":
		runPolicyFDExecHelper()
	case "reader":
		runPolicyFDReaderHelper()
	case "seccomp_exec":
		runSeccompFDExecHelper()
	case "seccomp_reader":
		runSeccompFDReaderHelper()
	}
}

func runPolicyFDExecHelper() {
	payload := []byte(os.Getenv("AIDE_TEST_POLICY_FD_PAYLOAD"))

	memFile, err := writePolicyToMemfd(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memfd: %v\n", err)
		os.Exit(2)
	}

	// Mirror the production launcher path: applyLandlock keeps the memfd at
	// its kernel-allocated fd (FD_CLOEXEC clear because MFD_CLOEXEC was not
	// set), encodes that fd number in argv, and the launcher calls
	// SyscallExecer.Exec → syscall.Exec. cmd.ExtraFiles is irrelevant because
	// syscall.Exec ignores it; the un-CLOEXEC'd fd is what survives.
	args := []string{os.Args[0], "-test.run=TestPolicyFD_SurvivesSyscallExec"}
	envv := withHelperEnv(map[string]string{
		"AIDE_TEST_POLICY_FD_MODE": "reader",
		"AIDE_TEST_POLICY_FD_NUM":  strconv.Itoa(int(memFile.Fd())),
	})
	if err := syscall.Exec(os.Args[0], args, envv); err != nil {
		fmt.Fprintf(os.Stderr, "syscall.Exec: %v\n", err)
		os.Exit(3)
	}
}

// withHelperEnv returns the current environment with all AIDE_TEST_POLICY_FD_*
// keys stripped, then sets overrides. Required because POSIX getenv returns the
// first match — append-only env construction would let the parent's MODE=exec
// shadow the helper's MODE=reader and cause infinite re-exec.
func withHelperEnv(overrides map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "AIDE_TEST_POLICY_FD_") {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func runPolicyFDReaderHelper() {
	fdStr := os.Getenv("AIDE_TEST_POLICY_FD_NUM")
	fdNum, err := strconv.Atoi(fdStr)
	if err != nil {
		fmt.Printf("BAD_FDNUM:%v\n", err)
		os.Exit(4)
	}
	f := os.NewFile(uintptr(fdNum), "policy-fd")
	data, rerr := io.ReadAll(f)
	if rerr != nil {
		fmt.Printf("READ_ERR:%v\n", rerr)
		os.Exit(5)
	}
	fmt.Printf("OK:%s", string(data))
	os.Exit(0)
}

// TestPolicyFD_SurvivesSyscallExec reproduces the production launcher path:
// applyLandlock places the memfd in cmd.ExtraFiles and the launcher then
// calls syscall.Exec. cmd.ExtraFiles is a Go *exec.Cmd-level abstraction
// honoured only by cmd.Start()/cmd.Run(); syscall.Exec is a raw libc call
// that ignores it. Combined with MFD_CLOEXEC on the memfd, the child wakes
// up with fd 3 already closed and read returns EBADF.
//
// This is the unit test gap that let DD-10 ship: the existing tests called
// readPolicyFromExtraFiles directly on the in-memory *os.File, which never
// crosses the exec boundary.
func TestPolicyFD_SurvivesSyscallExec(t *testing.T) {
	if os.Getenv("AIDE_TEST_POLICY_FD_MODE") != "" {
		t.Skip("subprocess helper invocation; primary assertion runs in parent")
	}

	const payload = `{"ProjectRoot":"/tmp/proj","Network":"none"}`

	cmd := exec.Command(os.Args[0], "-test.run=TestPolicyFD_SurvivesSyscallExec")
	cmd.Env = withHelperEnv(map[string]string{
		"AIDE_TEST_POLICY_FD_MODE":    "exec",
		"AIDE_TEST_POLICY_FD_PAYLOAD": payload,
	})
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Fatalf("subprocess helper failed: %v\noutput:\n%s", err, output)
	}

	want := "OK:" + payload
	if !strings.Contains(output, want) {
		t.Fatalf("policy fd lost across syscall.Exec boundary\n  want substring: %q\n  got output:     %q", want, output)
	}
}

// runSeccompFDExecHelper mirrors the launcher's bwrap path: invoke applyBwrap
// with AllowSubprocess=false so it allocates a seccomp memfd, extract the fd
// number from the resulting --seccomp argument, capture the expected BPF bytes,
// then syscall.Exec into the reader so we can prove the fd survives.
func runSeccompFDExecHelper() {
	s := &LinuxSandbox{}
	cmd := exec.Command("/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/aide-bwrap-fdtest",
		RuntimeDir:      "/tmp/aide-bwrap-fdtest",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: false,
	}
	// bwrap binary path is a placeholder — applyBwrap only writes it into argv,
	// it does not exec or stat it.
	if err := s.applyBwrap(cmd, policy, "/usr/bin/bwrap"); err != nil {
		fmt.Fprintf(os.Stderr, "applyBwrap: %v\n", err)
		os.Exit(2)
	}

	fdNum, ok := extractSeccompFD(cmd.Args)
	if !ok {
		fmt.Fprintf(os.Stderr, "no --seccomp <fd> in args: %v\n", cmd.Args)
		os.Exit(3)
	}

	if len(cmd.ExtraFiles) == 0 {
		fmt.Fprintln(os.Stderr, "cmd.ExtraFiles empty; expected seccomp memfd")
		os.Exit(4)
	}
	expectedBytes, err := io.ReadAll(cmd.ExtraFiles[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture expected BPF bytes: %v\n", err)
		os.Exit(5)
	}
	if _, err := cmd.ExtraFiles[0].Seek(0, io.SeekStart); err != nil {
		fmt.Fprintf(os.Stderr, "rewind memfd for reader: %v\n", err)
		os.Exit(6)
	}

	args := []string{os.Args[0], "-test.run=TestSeccompFD_SurvivesSyscallExec"}
	envv := withHelperEnv(map[string]string{
		"AIDE_TEST_POLICY_FD_MODE":  "seccomp_reader",
		"AIDE_TEST_POLICY_FD_NUM":   strconv.Itoa(fdNum),
		"AIDE_TEST_SECCOMP_EXPECT":  hex.EncodeToString(expectedBytes),
	})
	if err := syscall.Exec(os.Args[0], args, envv); err != nil {
		fmt.Fprintf(os.Stderr, "syscall.Exec: %v\n", err)
		os.Exit(7)
	}
}

// runSeccompFDReaderHelper runs in the exec'd-into image. It reads the fd
// referenced by --seccomp, compares to the BPF bytes the parent helper
// captured, and reports PASS/FAIL on stdout.
func runSeccompFDReaderHelper() {
	fdStr := os.Getenv("AIDE_TEST_POLICY_FD_NUM")
	fdNum, err := strconv.Atoi(fdStr)
	if err != nil {
		fmt.Printf("BAD_FDNUM:%v\n", err)
		os.Exit(4)
	}
	f := os.NewFile(uintptr(fdNum), "seccomp-fd")
	got, rerr := io.ReadAll(f)
	if rerr != nil {
		fmt.Printf("READ_ERR:%v\n", rerr)
		os.Exit(5)
	}
	want, err := hex.DecodeString(os.Getenv("AIDE_TEST_SECCOMP_EXPECT"))
	if err != nil {
		fmt.Printf("BAD_EXPECTED:%v\n", err)
		os.Exit(6)
	}
	if len(got) == 0 {
		fmt.Printf("EMPTY_READ: fd=%d had %d bytes (want %d)\n", fdNum, len(got), len(want))
		os.Exit(7)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		fmt.Printf("MISMATCH: got %d bytes (%s...), want %d bytes (%s...)\n",
			len(got), hex.EncodeToString(got)[:min(32, len(got)*2)],
			len(want), hex.EncodeToString(want)[:min(32, len(want)*2)])
		os.Exit(8)
	}
	fmt.Printf("PASS: read %d BPF bytes from fd %d\n", len(got), fdNum)
	os.Exit(0)
}

// extractSeccompFD scans args for "--seccomp" followed by a numeric fd.
// Returns the fd number and true on success.
func extractSeccompFD(args []string) (int, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "--seccomp" {
			continue
		}
		n, err := strconv.Atoi(args[i+1])
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// TestSeccompFD_SurvivesSyscallExec is the bwrap-path companion to
// TestPolicyFD_SurvivesSyscallExec. It exercises applyBwrap with
// AllowSubprocess=false (which allocates a seccomp BPF memfd and stamps
// --seccomp <fd> into bwrap's argv), then asserts that the fd encoded in argv
// is still readable in the post-syscall.Exec image and contains the same BPF
// bytes the parent wrote. Before commit 2946327, --seccomp was given the
// wrong fd number (3 + len(cmd.ExtraFiles), which is irrelevant under
// syscall.Exec) so bwrap would fail with EBADF or — worse — proceed without
// installing the filter, silently weakening AllowSubprocess=false.
func TestSeccompFD_SurvivesSyscallExec(t *testing.T) {
	if os.Getenv("AIDE_TEST_POLICY_FD_MODE") != "" {
		t.Skip("subprocess helper invocation; primary assertion runs in parent")
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSeccompFD_SurvivesSyscallExec")
	cmd.Env = withHelperEnv(map[string]string{
		"AIDE_TEST_POLICY_FD_MODE": "seccomp_exec",
	})
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		t.Fatalf("subprocess helper failed: %v\noutput:\n%s", err, output)
	}

	if !strings.Contains(output, "PASS:") {
		t.Fatalf("seccomp fd not reachable across syscall.Exec boundary\n  output: %q", output)
	}
}
