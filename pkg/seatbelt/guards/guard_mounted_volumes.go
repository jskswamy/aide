package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type mountedVolumesGuard struct{}

// MountedVolumesGuard returns a Guard that denies access to mounted volumes and external storage.
func MountedVolumesGuard() seatbelt.Guard { return &mountedVolumesGuard{} }

func (g *mountedVolumesGuard) Name() string { return "mounted-volumes" }
func (g *mountedVolumesGuard) Type() string { return "default" }
func (g *mountedVolumesGuard) Description() string {
	return "Blocks access to mounted volumes, Time Machine, and external storage"
}

func (g *mountedVolumesGuard) Rules(_ *seatbelt.Context) seatbelt.GuardResult {
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
