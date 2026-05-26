//go:build !linux

package sandbox

import "fmt"

func installNoSubprocessSeccomp() error {
	return fmt.Errorf("seccomp is Linux-only")
}

func noSubprocessSeccompMemfd() (releaser func(), fd int, err error) {
	return nil, 0, fmt.Errorf("seccomp is Linux-only")
}
