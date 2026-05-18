//go:build linux

package sandbox

import "github.com/jskswamy/aide/pkg/seatbelt"

func agentLinuxPaths(m seatbelt.Module, ctx *seatbelt.Context) (readable, writable []string) {
	if m == nil {
		return nil, nil
	}
	lpp, ok := m.(seatbelt.LinuxPathProvider)
	if !ok {
		return nil, nil
	}
	return lpp.LinuxReadable(ctx), lpp.LinuxWritable(ctx)
}
