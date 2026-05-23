//go:build linux

package sandbox

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// TestNoSubprocessSeccomp_BlocksSubprocess installs the no-subprocess seccomp
// filter in a child of the test binary, then attempts to launch /bin/true. The
// filter must block the underlying clone()-without-CLONE_THREAD syscall, so
// /bin/true never runs. The child reports its observation via stdout for the
// parent to inspect.
func TestNoSubprocessSeccomp_BlocksSubprocess(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("seccomp filter rejection semantics differ for uid 0; skipping")
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperSeccomp_BlocksFork$")
	cmd.Env = append(os.Environ(), "AIDE_TEST_SECCOMP=block_fork")
	out, _ := cmd.CombinedOutput()
	output := string(out)

	if strings.Contains(output, "INSTALL_FAILED") {
		t.Fatalf("seccomp install failed in child: %s", output)
	}
	if strings.Contains(output, "FORK_WORKED") {
		t.Errorf("seccomp should block subprocess creation, but /bin/true ran successfully; output: %s", output)
	}
	if !strings.Contains(output, "FORK_BLOCKED") {
		t.Errorf("expected FORK_BLOCKED indicator in child output; got: %s", output)
	}
}

// TestNoSubprocessSeccomp_AllowsThreads verifies the filter does not break
// thread creation (clone-with-CLONE_THREAD must remain allowed). Go agents
// require threading for the runtime itself.
func TestNoSubprocessSeccomp_AllowsThreads(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("seccomp filter rejection semantics differ for uid 0; skipping")
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperSeccomp_AllowsThreads$")
	cmd.Env = append(os.Environ(), "AIDE_TEST_SECCOMP=allow_threads")
	out, _ := cmd.CombinedOutput()
	output := string(out)

	if strings.Contains(output, "INSTALL_FAILED") {
		t.Fatalf("seccomp install failed in child: %s", output)
	}
	if !strings.Contains(output, "THREADS_OK") {
		t.Errorf("threading should still work with no-subprocess seccomp; got: %s", output)
	}
}

// TestBuildNoSubprocessFilter_ShapeAndSyscalls asserts the assembled filter
// has the expected static shape and references the syscalls we intend to
// gate. A regression here (e.g. accidentally allowing fork) would silently
// re-introduce the enforcement gap.
func TestBuildNoSubprocessFilter_ShapeAndSyscalls(t *testing.T) {
	raw, err := buildNoSubprocessFilter()
	if err != nil {
		t.Fatalf("buildNoSubprocessFilter: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("filter must not be empty")
	}
	// Sanity: the filter is small (single arch, four syscall checks, two
	// returns). Cap at a generous bound so accidental bloat is noticed.
	if len(raw) > 64 {
		t.Errorf("filter unexpectedly large (%d instructions); review for regressions", len(raw))
	}

	immediates := make(map[uint32]bool, len(raw))
	for _, ri := range raw {
		immediates[ri.K] = true
	}
	for _, want := range []struct {
		name string
		val  uint32
	}{
		{"AUDIT_ARCH_X86_64", auditArchX86_64},
		{"SYS_FORK", uint32(unix.SYS_FORK)},
		{"SYS_VFORK", uint32(unix.SYS_VFORK)},
		{"SYS_CLONE", uint32(unix.SYS_CLONE)},
		{"SYS_CLONE3", uint32(unix.SYS_CLONE3)},
		{"CLONE_THREAD", uint32(unix.CLONE_THREAD)},
		{"SECCOMP_RET_ALLOW", seccompRetAllow},
		{"SECCOMP_RET_ERRNO|EPERM", seccompRetErrnoEPERM},
	} {
		if !immediates[want.val] {
			t.Errorf("filter does not reference %s (0x%x); expected at least one BPF immediate matching", want.name, want.val)
		}
	}
}

// TestNoSubprocessSeccompMemfd_ContentMatchesBuilder asserts the memfd
// produced for handing to bwrap --seccomp contains the same BPF bytes as
// the in-process filter built by buildNoSubprocessFilter. A drift between
// the two would mean bwrap-installed sandboxes and Landlock-installed
// sandboxes enforce different rules.
func TestNoSubprocessSeccompMemfd_ContentMatchesBuilder(t *testing.T) {
	memFile, err := noSubprocessSeccompMemfd()
	if err != nil {
		t.Fatalf("noSubprocessSeccompMemfd: %v", err)
	}
	t.Cleanup(func() { _ = memFile.Close() })

	got, err := io.ReadAll(memFile)
	if err != nil {
		t.Fatalf("read memfd: %v", err)
	}

	raw, err := buildNoSubprocessFilter()
	if err != nil {
		t.Fatalf("buildNoSubprocessFilter: %v", err)
	}
	if len(got) != len(raw)*8 {
		t.Fatalf("memfd byte length %d != %d*8", len(got), len(raw))
	}

	// Decode the memfd bytes back into sock_filter records and compare.
	for i, ri := range raw {
		offset := i * 8
		op := binary.LittleEndian.Uint16(got[offset : offset+2])
		jt := got[offset+2]
		jf := got[offset+3]
		k := binary.LittleEndian.Uint32(got[offset+4 : offset+8])
		if op != ri.Op || jt != ri.Jt || jf != ri.Jf || k != ri.K {
			t.Errorf("instruction %d mismatch: memfd={op=%#x jt=%d jf=%d k=%#x} builder={op=%#x jt=%d jf=%d k=%#x}",
				i, op, jt, jf, k, ri.Op, ri.Jt, ri.Jf, ri.K)
		}
	}
}

// TestHelperSeccomp_BlocksFork is invoked as a child process. It installs the
// no-subprocess seccomp filter, then tries to spawn /bin/true. The parent
// inspects this process's stdout to observe the seccomp filter's effect.
func TestHelperSeccomp_BlocksFork(_ *testing.T) {
	if os.Getenv("AIDE_TEST_SECCOMP") != "block_fork" {
		return
	}
	if err := installNoSubprocessSeccomp(); err != nil {
		fmt.Fprintln(os.Stdout, "INSTALL_FAILED:", err)
		os.Exit(0)
	}
	err := exec.Command("/bin/true").Run()
	if err != nil {
		fmt.Fprintln(os.Stdout, "FORK_BLOCKED:", err)
	} else {
		fmt.Fprintln(os.Stdout, "FORK_WORKED")
	}
	os.Exit(0)
}

// TestHelperSeccomp_AllowsThreads is invoked as a child process. It installs
// the seccomp filter then forces creation of a new OS thread via a
// LockOSThread'd goroutine. Bumping GOMAXPROCS first makes the Go runtime
// likely to spawn a fresh thread under the filter rather than reusing an
// existing one created before install.
func TestHelperSeccomp_AllowsThreads(_ *testing.T) {
	if os.Getenv("AIDE_TEST_SECCOMP") != "allow_threads" {
		return
	}
	runtime.GOMAXPROCS(8)
	if err := installNoSubprocessSeccomp(); err != nil {
		fmt.Fprintln(os.Stdout, "INSTALL_FAILED:", err)
		os.Exit(0)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer wg.Done()
			// Touch some memory under the OS thread we're locked to.
			_ = make([]byte, 4096)
		}()
	}
	wg.Wait()
	fmt.Fprintln(os.Stdout, "THREADS_OK")
	os.Exit(0)
}
