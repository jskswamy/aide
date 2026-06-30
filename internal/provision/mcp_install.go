package provision

import (
	"context"
	"fmt"
)

// InstallMCP handles the shared MCP install lifecycle: validate, pre-remove
// for idempotency, build argv via buildArgs, and invoke RunCLI.
// buildArgs is called once with the MCPServer and must return the argv
// for the install command (without binary name) and any error.
// preRemoveArgs is the argv for the pre-remove call (without binary name,
// e.g., ["mcp", "remove", name] or ["mcp", "remove", name, "-s", "user"]).
// extraTolerate is an optional list of stderr tokens to tolerate on failure.
func InstallMCP(ctx context.Context, r Runner, env map[string]string, bin string, s MCPServer,
	buildArgs func(s MCPServer) ([]string, error),
	preRemoveArgs []string,
	extraTolerate ...string) error {

	if s.URL == "" && s.Command == "" {
		return fmt.Errorf("server %q has neither URL nor Command", s.Key)
	}
	// Idempotency: pre-remove (tolerating not-found).
	_, _, _, _ = r.Run(ctx, env, bin, preRemoveArgs...)

	args, err := buildArgs(s)
	if err != nil {
		return err
	}
	tolerate := append([]string{}, DefaultTolerateStderr...)
	tolerate = append(tolerate, extraTolerate...)
	return RunCLI(ctx, r, env, "install mcp "+s.Key, bin, args, tolerate...)
}

// UninstallMCP handles the shared MCP uninstall lifecycle: invoke RunCLI
// with pre-built remove args and an optional set of extra-tolerate phrases.
// removeArgs is the argv for the remove command (without binary name).
// extraTolerate is an optional list of stderr tokens to tolerate on failure.
func UninstallMCP(ctx context.Context, r Runner, env map[string]string, bin, name string,
	removeArgs []string,
	extraTolerate ...string) error {

	tolerate := append([]string{}, DefaultTolerateStderr...)
	tolerate = append(tolerate, extraTolerate...)
	return RunCLI(ctx, r, env, "uninstall mcp "+name, bin, removeArgs, tolerate...)
}
