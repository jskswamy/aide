// cmd/aide/cmdenv.go
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
)

// Env captures the typical CLI subcommand preamble: cwd + loaded
// config, plus lazy access to the merged capability registry.
//
// Contract: after cmdEnv returns, Env.Config() is always non-nil.
// Any load failure is returned as err; callers choose their policy:
//
//   - strict:         if err != nil { return err }
//   - best-effort:    env, _ := cmdEnv(cmd)
//   - defer-validate: env, loadErr := cmdEnv(cmd); report loadErr later
//   - check-only:     _, err := cmdEnv(cmd); if err != nil { ... }
type Env struct {
	cmd      *cobra.Command
	cwd      string
	cfg      *config.Config
	registry map[string]capability.Capability
	regBuilt bool
}

// cmdEnv resolves the working directory and loads the aide config.
// On filesystem failure (e.g. os.Getwd errors) Env.Config() still
// returns a non-nil empty Config so callers can proceed safely.
func cmdEnv(cmd *cobra.Command) (*Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return &Env{cmd: cmd, cfg: &config.Config{}}, err
	}
	cfg, loadErr := config.Load(config.Dir(), cwd)
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &Env{cmd: cmd, cwd: cwd, cfg: cfg}, loadErr
}

// CWD returns the working directory captured at construction.
func (e *Env) CWD() string { return e.cwd }

// Config returns the loaded config. Never nil; on load failure it is
// an empty Config{} so best-effort callers can proceed.
func (e *Env) Config() *config.Config { return e.cfg }

// Registry returns the merged capability registry (built-ins plus
// user-defined capabilities). Built on first call and memoized;
// non-cap commands that never call Registry pay no construction
// cost.
func (e *Env) Registry() map[string]capability.Capability {
	if !e.regBuilt {
		userCaps := capability.FromConfigDefs(e.cfg.Capabilities)
		e.registry = capability.MergedRegistry(userCaps)
		e.regBuilt = true
	}
	return e.registry
}
