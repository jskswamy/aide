package main

import (
	"os"
	"testing"
)

// TestMain keeps state files under each test's HOME: an
// XDG_STATE_HOME exported by the developer's shell would otherwise
// send them to the real state directory.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("XDG_STATE_HOME")
	os.Exit(m.Run())
}
