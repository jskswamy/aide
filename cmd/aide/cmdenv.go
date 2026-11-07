// cmd/aide/cmdenv.go
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
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
// On any failure Env.Config() is a non-nil empty config.FailedLoad
// placeholder: best-effort callers can read it, but config.WriteConfig
// refuses it, so ignoring the error can never overwrite the user's
// config.yaml with an empty one.
func cmdEnv(cmd *cobra.Command) (*Env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return &Env{cmd: cmd, cfg: config.FailedLoad(err)}, err
	}
	cfg, loadErr := config.Load(config.Dir(), cwd)
	if cfg == nil {
		cfg = config.FailedLoad(loadErr)
	}
	if loadErr == nil && cfg.LegacyMonolithic && shouldNudgeForCmd(cmd) {
		home, _ := os.UserHomeDir()
		errOut := io.Writer(os.Stderr)
		if cmd != nil {
			errOut = cmd.ErrOrStderr()
		}
		maybeNudgeConfigMigration(cfg, errOut, isInteractiveTerminal(os.Stderr), home, time.Now())
	}
	return &Env{cmd: cmd, cwd: cwd, cfg: cfg}, loadErr
}

// CWD returns the working directory captured at construction.
func (e *Env) CWD() string { return e.cwd }

// Config returns the loaded config. Never nil; on load failure it is
// an empty, unwritable config.FailedLoad placeholder.
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

// ResolveContextForMutation resolves the target context name (defaulting to
// the active context for e.cwd when contextName is empty) and returns the
// loaded config, resolved context name, and Context struct for mutation.
func (e *Env) ResolveContextForMutation(contextName string) (*config.Config, string, config.Context, error) {
	if contextName == "" {
		rc, err := aidectx.Resolve(e.cfg, e.cwd, aidectx.GitRemotes(e.cwd))
		if err != nil {
			return nil, "", config.Context{}, fmt.Errorf("resolving profile: %w", err)
		}
		contextName = rc.Name
	}
	ctx, ok := e.cfg.Contexts[contextName]
	if !ok {
		return nil, "", config.Context{}, fmt.Errorf("context %q not found", contextName)
	}
	return e.cfg, contextName, ctx, nil
}

// ResolveProjectOverrideForMutation resolves the project override file to
// edit for e.cwd (or the main checkout when main is true) and returns the
// loaded global config, the raw ProjectOverride, and the target file path.
func (e *Env) ResolveProjectOverrideForMutation(main bool) (*config.Config, *config.ProjectOverride, string, error) {
	poPath, err := config.ProjectConfigWriteTarget(e.cwd, main)
	if err != nil {
		return nil, nil, "", err
	}
	po, err := config.LoadProjectOverrideFile(poPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading project config %s: %w", poPath, err)
	}
	return e.cfg, po, poPath, nil
}

