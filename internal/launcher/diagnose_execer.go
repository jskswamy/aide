//go:build !windows

package launcher

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DiagnoseExecer runs the child via fork+exec so aide stays alive to gather
// post-mortem data. Used only when --diagnose is set; the default path
// remains SyscallExecer (process replacement).
type DiagnoseExecer struct {
	StderrLineLimit int   // 0 → no line cap
	StderrByteLimit int64 // 0 → no byte cap
}

// Run executes binary with args and env, returning observed run state.
//
// extraFiles is forwarded via cmd.ExtraFiles so the child inherits them at
// fds 3, 4, .... When non-empty, argv is also remapped so any embedded
// parent-side memfd numbers (e.g. --policy-fd=42 or --seccomp 42) become
// the matching child-side fd (3 + index). See remapArgvFDs for the
// argv-format contract.
func (d *DiagnoseExecer) Run(binary string, args []string, env []string, extraFiles []*os.File) (*RunResult, error) {
	args = remapArgvFDs(args, extraFiles)

	cmd := exec.Command(binary, args[1:]...)
	cmd.Path = binary
	cmd.Args = args
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.ExtraFiles = extraFiles

	// Intentionally NOT setting Setpgid: keeping the child in aide's
	// process group means it shares the TTY's foreground group, so
	// claude's TUI passes its tcgetpgrp == getpgrp foreground check and
	// renders normally, and Ctrl+C from the keyboard reaches the child
	// directly via the kernel. signal.Notify in the parent prevents aide
	// itself from being terminated by the same signal so cmd.Wait can
	// return cleanly.

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	childPid := cmd.Process.Pid

	tail, truncated := captureStderr(stderrPipe, os.Stderr, d.StderrLineLimit, d.StderrByteLimit)

	stopSignals := forwardSignals(cmd.Process)

	// Drain stderr before cmd.Wait(): StderrPipe registers the read end in
	// closeAfterWait, which races with the capture goroutine still reading.
	stderrTail := <-tail
	stderrTruncated := <-truncated

	err = cmd.Wait()
	close(stopSignals)

	res := &RunResult{
		Runtime:              time.Since(start),
		StderrTail:           stderrTail,
		StderrTruncatedBytes: stderrTruncated,
		Pid:                  childPid,
	}
	if err == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		ws, ok2 := exitErr.Sys().(syscall.WaitStatus)
		if ok2 {
			res.ExitCode = ws.ExitStatus()
			if ws.Signaled() {
				res.Signal = signalName(ws.Signal())
				res.ExitCode = 128 + int(ws.Signal())
			}
		} else {
			res.ExitCode = exitErr.ExitCode()
		}
		return res, nil
	}
	return nil, err
}

// remapArgvFDs rewrites parent-side memfd numbers embedded in argv to the
// child-side numbers that exec.Cmd.Start assigns via ExtraFiles
// (extraFiles[i] becomes child fd 3+i).
//
// The Linux sandbox layer (sandbox.applyLandlock / applyBwrap) embeds the
// parent's actual memfd fd in argv on the assumption that the launcher
// will use syscall.Exec, which preserves fd numbers across process image
// replacement. Under --diagnose the launcher uses cmd.Start instead, which
// dup2s ExtraFiles[i] to fd 3+i in the child — the parent-side number is
// no longer valid, and without remapping the child reads a closed fd and
// the sandbox setup fails (Landlock: "bad file descriptor"; bwrap: filter
// load failure).
//
// Two argv formats are matched, mirroring the sandbox emit sites:
//
//   - "--policy-fd=N"  (joined; Landlock policy via aide __sandbox-apply)
//   - "--seccomp N"    (split;  bwrap seccomp BPF)
//
// Matching is anchored on the exact flag string so accidental occurrences
// of the parent fd number elsewhere in argv (unlikely but possible) are
// not rewritten.
func remapArgvFDs(args []string, extraFiles []*os.File) []string {
	if len(extraFiles) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	for i, f := range extraFiles {
		parent := strconv.Itoa(int(f.Fd()))
		child := strconv.Itoa(3 + i)
		for j := range out {
			if out[j] == "--policy-fd="+parent {
				out[j] = "--policy-fd=" + child
				continue
			}
			if out[j] == "--seccomp" && j+1 < len(out) && out[j+1] == parent {
				out[j+1] = child
			}
		}
	}
	return out
}

// captureStderr tees from src to passthrough while collecting up to lineLimit
// lines and byteLimit bytes into a ring. Returns channels that yield the
// final tail and truncated-byte count once stderr closes.
func captureStderr(src io.Reader, passthrough io.Writer, lineLimit int, byteLimit int64) (<-chan string, <-chan int64) {
	tailCh := make(chan string, 1)
	truncCh := make(chan int64, 1)

	go func() {
		var (
			lines     []string
			truncated int64
		)

		reader := bufio.NewReader(src)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				_, _ = passthrough.Write([]byte(line))
				lines = append(lines, line)
				if lineLimit > 0 && len(lines) > lineLimit {
					truncated += int64(len(lines[0]))
					lines = lines[1:]
				}
				if byteLimit > 0 {
					var total int64
					for _, l := range lines {
						total += int64(len(l))
					}
					for total > byteLimit && len(lines) > 1 {
						truncated += int64(len(lines[0]))
						total -= int64(len(lines[0]))
						lines = lines[1:]
					}
				}
			}
			if err != nil {
				break
			}
		}
		tail := strings.Join(lines, "")
		if truncated > 0 {
			tail = "[…stderr truncated, " + strconv.FormatInt(truncated, 10) + " bytes dropped…]\n" + tail
		}
		tailCh <- tail
		truncCh <- truncated
	}()

	return tailCh, truncCh
}

func forwardSignals(p *os.Process) chan struct{} {
	stop := make(chan struct{})
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-stop:
				signal.Stop(ch)
				return
			case s := <-ch:
				_ = p.Signal(s)
			}
		}
	}()
	return stop
}

func signalName(s syscall.Signal) string {
	switch s {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return s.String()
	}
}

