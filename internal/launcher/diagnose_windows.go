//go:build windows

package launcher

import "errors"

// runDiagnose is a stub on Windows. The fork+exec capture path relies on
// SIGWINCH and process-group setup that aren't available there.
func (l *Launcher) runDiagnose(binary string, args, env []string, dc diagContext) error {
	return errors.New("--diagnose is not supported on Windows")
}
