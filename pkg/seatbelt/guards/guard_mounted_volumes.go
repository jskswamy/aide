package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type mountedVolumesGuard struct{}

func MountedVolumesGuard() seatbelt.Guard { return &mountedVolumesGuard{} }

func (g *mountedVolumesGuard) Name() string { return "mounted-volumes" }
func (g *mountedVolumesGuard) Type() string { return "default" }
func (g *mountedVolumesGuard) Description() string {
	return "Blocks access to mounted volumes, Time Machine, and external storage"
}

func (g *mountedVolumesGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if !dirExists("/Volumes") {
		return seatbelt.GuardResult{
			Skipped: []string{"/Volumes not found"},
		}
	}
	return seatbelt.GuardResult{
		Rules:     DenyDir("/Volumes"),
		Protected: []string{"/Volumes"},
	}
}
