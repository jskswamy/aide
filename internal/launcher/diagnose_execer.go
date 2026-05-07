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
func (d *DiagnoseExecer) Run(binary string, args []string, env []string) (*RunResult, error) {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Path = binary
	cmd.Args = args
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

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

	err = cmd.Wait()
	close(stopSignals)

	res := &RunResult{
		Runtime:              time.Since(start),
		StderrTail:           <-tail,
		StderrTruncatedBytes: <-truncated,
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

