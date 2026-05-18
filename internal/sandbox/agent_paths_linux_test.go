//go:build linux

package sandbox

import (
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type stubLinuxPathProvider struct {
	readable []string
	writable []string
}

func (s *stubLinuxPathProvider) Name() string                              { return "stub-path" }
func (s *stubLinuxPathProvider) Rules(*seatbelt.Context) seatbelt.GuardResult { return seatbelt.GuardResult{} }
func (s *stubLinuxPathProvider) LinuxReadable(*seatbelt.Context) []string  { return s.readable }
func (s *stubLinuxPathProvider) LinuxWritable(*seatbelt.Context) []string  { return s.writable }

var _ seatbelt.LinuxPathProvider = (*stubLinuxPathProvider)(nil)

func TestAgentLinuxPaths_NilModule(t *testing.T) {
	r, w := agentLinuxPaths(nil, nil)
	if r != nil || w != nil {
		t.Errorf("nil module: got (%v, %v), want (nil, nil)", r, w)
	}
}

func TestAgentLinuxPaths_NotAProvider(t *testing.T) {
	// stubAtomicWriteModule implements Module but not LinuxPathProvider.
	mod := &stubAtomicWriteModule{}
	r, w := agentLinuxPaths(mod, nil)
	if r != nil || w != nil {
		t.Errorf("non-provider: got (%v, %v), want (nil, nil)", r, w)
	}
}

func TestAgentLinuxPaths_Provider(t *testing.T) {
	mod := &stubLinuxPathProvider{
		readable: []string{"/home/u/.config"},
		writable: []string{"/home/u/.local"},
	}
	ctx := &seatbelt.Context{HomeDir: "/home/u"}
	r, w := agentLinuxPaths(mod, ctx)
	if len(r) != 1 || r[0] != "/home/u/.config" {
		t.Errorf("readable: got %v, want [/home/u/.config]", r)
	}
	if len(w) != 1 || w[0] != "/home/u/.local" {
		t.Errorf("writable: got %v, want [/home/u/.local]", w)
	}
}
