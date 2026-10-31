//go:build darwin || linux

package piinstall_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
)

func TestDiscoverPrivateRootNotRead(t *testing.T) {
	f := testfixture.New(t)
	marker := filepath.Join(f.Root, "managed-install.json")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(marker, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := piinstall.Discover(f.Launcher, f.Home, nil, []string{f.Root}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("private root accepted")
		}
	case <-time.After(2 * time.Second):
		// Release an incorrect blocking read so the regression leaves no goroutine.
		fd, err := os.OpenFile(marker, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		<-done
		_ = fd.Close()
		t.Fatal("discovery read private metadata before rejecting its root")
	}
}
