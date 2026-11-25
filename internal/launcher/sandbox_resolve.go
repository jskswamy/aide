package launcher

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/sandbox"
)

// SandboxInputs are what the capability and sandbox step reads besides
// the config and the selected profile.
type SandboxInputs struct {
	ProjectRoot string
	HomeDir     string
	// Getenv reads the environment the session will see (launch layers
	// the profile env over the process env); nil means os.Getenv.
	Getenv              func(string) string
	WithCaps            []string // --with
	WithoutCaps         []string // --without
	UnrestrictedNetwork bool     // -N
	// Variants carries consent, prompting and --variant overrides;
	// ProjectRoot, FS and the .aide.yaml pins are filled in here.
	Variants sandbox.VariantSelectionOptions
}

// SandboxResolution is the profile's capabilities merged into its
// sandbox config, as launch applies them.
type SandboxResolution struct {
	Caps        *capability.Set
	ContextCaps map[string]bool // capabilities the profile itself lists
	Overrides   config.SandboxOverrides
	Provenance  map[string]capability.Provenance
	// Config is the sandbox config after capability overrides and -N.
	Config   *config.SandboxPolicy
	Disabled bool
	// The profile's own extra paths, before capability overrides.
	ConfigWritable, ConfigReadable, ConfigDenied []string
}

// ResolveSandbox is the step launch runs after selection: merge the
// capability names, resolve the sandbox reference, resolve
// capabilities with variants, apply their overrides and -N. Launch,
// which, status and doctor share it so they agree on the sandbox. On
// error it returns what it resolved so far.
func ResolveSandbox(cfg *config.Config, rc *aidectx.ResolvedContext, in SandboxInputs) (SandboxResolution, error) {
	var res SandboxResolution
	capNames := sandbox.MergeCapNames(rc.Context.Capabilities, in.WithCaps, in.WithoutCaps)
	excluded := slices.Clone(in.WithoutCaps)
	if cfg.ProjectOverride != nil {
		excluded = append(excluded, cfg.ProjectOverride.DisabledCapabilities...)
	}
	getenv := in.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	capNames = capability.AutoInclude(capNames, excluded, in.HomeDir, getenv)

	res.ContextCaps = make(map[string]bool, len(rc.Context.Capabilities))
	for _, c := range rc.Context.Capabilities {
		res.ContextCaps[c] = true
	}

	var err error
	res.Config, res.Disabled, err = sandbox.ResolveSandboxRef(rc.Context.Sandbox, cfg.Sandboxes)
	if err != nil {
		return res, fmt.Errorf("resolving sandbox: %w", err)
	}
	if res.Config != nil {
		res.ConfigWritable = append([]string{}, res.Config.WritableExtra...)
		res.ConfigReadable = append([]string{}, res.Config.ReadableExtra...)
		res.ConfigDenied = append([]string{}, res.Config.DeniedExtra...)
	}

	opts := in.Variants
	opts.ProjectRoot = in.ProjectRoot
	opts.FS = os.DirFS(in.ProjectRoot)
	opts.YAMLPins = yamlVariantPins(cfg.ProjectOverride)
	res.Caps, res.Overrides, res.Provenance, err = sandbox.ResolveCapabilitiesWithVariants(capNames, cfg, opts)
	if err != nil {
		return res, fmt.Errorf("resolving capabilities: %w", err)
	}
	sandbox.ApplyOverrides(&res.Config, res.Overrides)

	if in.UnrestrictedNetwork {
		if res.Config == nil {
			res.Config = &config.SandboxPolicy{}
		}
		if res.Config.Network == nil {
			res.Config.Network = &config.NetworkPolicy{}
		}
		res.Config.Network.Mode = "unrestricted"
		res.Config.Network.AllowPorts = nil
		res.Config.Network.DenyPorts = nil
	}
	return res, nil
}

// PreviewGetenv approximates the session environment launch builds,
// without decrypting: the profile's env values that need no template
// (with ~ expanded) over the process env. Previews (which, status,
// doctor) pass it as SandboxInputs.Getenv and Variants.EnvLookup.
func PreviewGetenv(env map[string]string, home string) func(string) string {
	return func(k string) string {
		if v, ok := env[k]; ok && !strings.Contains(v, "{{") {
			return homepath.Expand(v, home)
		}
		return os.Getenv(k)
	}
}
