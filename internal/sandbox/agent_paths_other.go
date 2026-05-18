//go:build !linux

package sandbox

import "github.com/jskswamy/aide/pkg/seatbelt"

// seatbelt.LinuxPathProvider does not exist off Linux, so this is a stub.
func agentLinuxPaths(_ seatbelt.Module, _ *seatbelt.Context) (readable, writable []string) {
	return nil, nil
}
