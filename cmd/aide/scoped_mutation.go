package main

import (
	"fmt"
	"io"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/trust"
)

// scopedMutation describes a config edit that targets either the user-level
// (global) Context inside ~/.config/aide/config.yaml or the project-level
// override file. Each command supplies the per-scope mutate closure plus the
// success-message formatters.
type scopedMutation struct {
	contextMutate  func(ctx *config.Context) error
	projectMutate  func(po *config.ProjectOverride) error
	successGlobal  func(ctxName string) string
	successProject func(poPath string) string
}

// validateContextScope rejects the "--context without --global" combination
// that 14 cobra commands all guarded against by hand.
func validateContextScope(global bool, contextName string) error {
	if !global && contextName != "" {
		return fmt.Errorf("the --context flag requires --global")
	}
	return nil
}

// runScopedMutation dispatches m to the global or project edit path, persists
// the result, and prints the matching success message. Callers should still
// validate any command-specific flags (e.g. network-mode value) before
// invoking this helper.
func runScopedMutation(out io.Writer, global bool, contextName string, m scopedMutation) error {
	if err := validateContextScope(global, contextName); err != nil {
		return err
	}
	if global {
		cfg, ctxName, ctx, err := resolveContextForMutation(contextName)
		if err != nil {
			return err
		}
		if err := m.contextMutate(&ctx); err != nil {
			return err
		}
		cfg.Contexts[ctxName] = ctx
		if err := config.WriteConfig(cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Fprintln(out, m.successGlobal(ctxName))
		return nil
	}
	_, po, poPath, err := resolveProjectOverrideForMutation()
	if err != nil {
		return err
	}
	if err := m.projectMutate(po); err != nil {
		return err
	}
	if err := config.WriteProjectOverrideWithTrust(poPath, po, trust.DefaultStore()); err != nil {
		return fmt.Errorf("writing project config: %w", err)
	}
	fmt.Fprintln(out, m.successProject(poPath))
	return nil
}
