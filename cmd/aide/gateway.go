// cmd/aide/gateway.go
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/gatewaypreset"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/ui"
)

// GatewayListEntry is one gateway or provider credential in `aide gateway list --format json`.
type GatewayListEntry struct {
	Profile    string                      `json:"profile"`
	Gateway    string                      `json:"gateway"`
	Kind       string                      `json:"kind,omitempty"`
	BaseURL    string                      `json:"base_url"`
	Store      string                      `json:"store"`
	Key        string                      `json:"key"`
	RefreshTTL string                      `json:"refresh_ttl,omitempty"`
	CachePath  string                      `json:"cache_path,omitempty"`
	FetchedAt  *time.Time                  `json:"fetched_at,omitempty"`
	Expired    bool                        `json:"expired"`
	ModelCount int                         `json:"model_count"`
	DefaultFor []string                    `json:"default_for"`
	Harnesses  []gateway.HarnessFileStatus `json:"harnesses"`
	Models     []gateway.Model             `json:"models,omitempty"`
	// ChosenFor lists the harnesses whose resolved choice is this
	// gateway, with where the choice came from.
	ChosenFor []GatewayChoice `json:"chosen_for"`

	URLMismatch bool `json:"-"`
	ttl         time.Duration
}

// GatewayChoice is one harness's choice of a gateway and its source.
type GatewayChoice struct {
	Harness string `json:"harness"`
	Source  string `json:"source"`
}

// chosenFor maps each gateway of profile p to the harnesses that
// resolve to it from the profile's own configuration (a repository
// preference is not read here).
func chosenFor(cfg *config.Config, p string, ctx config.Context) map[string][]GatewayChoice {
	out := map[string][]GatewayChoice{}
	for _, d := range harness.All() {
		if d.Reach == harness.ReachNone {
			continue
		}
		if c, _, err := launcher.ResolveChoice(cfg, p, ctx, d.Name, launcher.SelectionInputs{}, nil); err == nil && c.Value != "" {
			out[c.Value] = append(out[c.Value], GatewayChoice{Harness: d.Name, Source: ui.SourceLabel(c.Source, p)})
		}
	}
	return out
}

func gatewayCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Manage custom LLM gateways and sync their models into harness configs",
		Long: `Manage custom LLM gateways and synchronize model catalogues across harnesses.

A gateway is an Anthropic- and OpenAI-compatible endpoint (for example
LiteLLM) that aide injects into claude, pi, opencode and codex. Its
bearer token lives in an encrypted secret store; harness config files
only name the variable aide injects it as.`,
	}
	cmd.PersistentFlags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	_ = cmd.RegisterFlagCompletionFunc("profile", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return profileNamesForCompletion(), cobra.ShellCompDirectiveNoFileComp
	})
	cmd.AddCommand(gatewayAddCmd(&profile))
	cmd.AddCommand(gatewayListCmd(&profile))
	cmd.AddCommand(gatewayUseCmd(&profile))
	cmd.AddCommand(gatewayEditCmd(&profile))
	cmd.AddCommand(gatewaySyncCmd(&profile))
	cmd.AddCommand(gatewayRemoveCmd(&profile))
	return cmd
}

func gatewayAddCmd(profile *string) *cobra.Command {
	var (
		rawURL, ttl, harnesses, store, key, ageKey, fromEnv, model, api string
		defaultFor                                                      []string
		tokenStdin, force, makeDefault, noAuth, generateAgeKey          bool
	)
	cmd := &cobra.Command{
		Use:   "add [<preset>|<name>]",
		Short: "Register a gateway or built-in preset, encrypt its key, and sync models",
		Long: `Register a gateway on a profile, encrypt its key into the profile's
secret store, and sync its models into harness configs.

A built-in preset (for example openrouter-anthropic) supplies the URL, wire
protocol and compatible harnesses; a custom gateway takes --url. The key
is read from a hidden prompt, from stdin (--key-stdin) or from an
environment variable (--from-env, or --from-env=VAR); never from a flag
value, which would land in shell history. A bare --from-env on a preset
reads the first of the preset's usual variables that is set.

For each marked claude or codex, --default-for <h> makes this gateway the
profile's choice; otherwise an unset choice is saved as none, so launches
keep using the harness's own login.

Examples:
  aide gateway add openrouter-anthropic --from-env --profile work
  aide gateway add corp --url https://gateway.example.com --profile work
  op read op://Eng/Gateway/credential | aide gateway add corp --url https://gateway.example.com --key-stdin
  aide gateway add corp --url https://gateway.example.com --from-env=CORP_GATEWAY_TOKEN --harnesses claude,pi --default-for claude`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokenStdin && fromEnv != "" {
				return errors.New("--key-stdin and --from-env are mutually exclusive")
			}
			if noAuth && (tokenStdin || fromEnv != "") {
				return errors.New("--no-auth takes no key; drop --key-stdin and --from-env")
			}
			if noAuth && (key != "" || store != "") {
				return errors.New("--no-auth takes no key; drop --key and --secret-store")
			}
			if api != "" && api != string(gatewaypreset.APIAnthropicMessages) {
				return fmt.Errorf("--api %s: %w", api, errWireNotDelivered)
			}
			for _, h := range defaultFor {
				if !slices.Contains(config.DefaultGatewayHarnesses, h) {
					return fmt.Errorf("--default-for %q is not a gateway harness (use %s)", h, strings.Join(config.DefaultGatewayHarnesses, ", "))
				}
			}
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(cmd.InOrStdin())
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg, err := env.MigratedConfig()
			if err != nil {
				return err
			}
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), *profile)
			if err != nil {
				return err
			}
			var preset gatewaypreset.Preset
			var isPreset bool
			switch {
			case rawURL != "":
			case len(args) == 1:
				preset, isPreset = gatewaypreset.Lookup(args[0])
			case fromEnv != "" && fromEnv != fromEnvAuto:
				for _, p := range gatewaypreset.All() {
					if slices.Contains(p.ShellVars, fromEnv) && presetDeliverable(p) == nil {
						preset, isPreset, args = p, true, []string{p.Name}
						break
					}
				}
			}
			var presetHarnesses []string
			if isPreset {
				if err := presetDeliverable(preset); err != nil {
					return err
				}
				rawURL, presetHarnesses = preset.BaseURL, preset.Harnesses
				if api == "" {
					api = string(preset.API)
				}
			}
			if fromEnv == fromEnvAuto {
				if fromEnv, err = presetShellVar(preset, isPreset); err != nil {
					return err
				}
			}
			tokenEnv := fromEnv
			if !isPreset {
				rawURL, err = readGatewayURL(cmd, reader, rawURL, tokenStdin)
				if err != nil {
					return err
				}
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				derived, deriveErr := gatewayNameFromURL(rawURL)
				switch {
				case !tokenStdin && stdinIsTTY():
					name, err = readGatewayName(cmd, reader, derived)
					if err != nil {
						return err
					}
				case deriveErr != nil:
					return deriveErr
				default:
					name = derived
				}
			}

			resolvedHarnesses, err := resolveGatewayAddHarnesses(cmd, reader, harnesses, cmd.Flags().Changed("harnesses"), tokenStdin, presetHarnesses)
			if err != nil {
				return err
			}

			gw := config.GatewayConfig{
				BaseURL:    gateway.NormalizeBaseURL(rawURL),
				Model:      model,
				Preset:     preset.Name,
				API:        api,
				RefreshTTL: ttl,
				Store:      store,
				Key:        key,
				Harnesses:  resolvedHarnesses,
			}
			if gw.Key != "" && gw.Store == "" {
				gw.Store = ctx.Secret
				if gw.Store == "" {
					gw.Store = profileName
				}
			}
			if gw.Store != "" && gw.Key == "" {
				gw.Key = defaultGatewayKey(name)
			}
			old, hadOld := ctx.Gateways[name]
			if hadOld && !force && gateway.NormalizeBaseURL(old.BaseURL) != gw.BaseURL {
				return fmt.Errorf("profile %q already has gateway %q for %s; pass --force to replace it (and its token), or give this one another name",
					profileName, name, old.BaseURL)
			}

			ctx.Gateways = maps.Clone(ctx.Gateways)
			if ctx.Gateways == nil {
				ctx.Gateways = map[string]config.GatewayConfig{}
			}
			ctx.Gateways[name] = gw
			if err := config.ValidateGateways("profiles."+profileName, ctx, cfg.NeverAllowEnv); err != nil {
				return err
			}

			fctx, cancel := context.WithTimeout(context.Background(), gateway.DefaultFetchTimeout)
			if noAuth && !gw.IsLoopback() {
				cancel()
				return errors.New("--no-auth is only for loopback URLs (localhost, 127.0.0.1 or ::1)")
			}
			token, models, err := resolveGatewayAddAuth(fctx, cmd, reader, gw, tokenStdin, tokenEnv, noAuth)
			cancel()
			if err != nil {
				return err
			}
			if token != "" {
				if gw.Store == "" {
					gw.Store = ctx.Secret
				}
				if gw.Store == "" {
					gw.Store = profileName
				}
				if gw.Key == "" {
					gw.Key = defaultGatewayKey(name)
				}
				ctx.Gateways[name] = gw
				if err := config.ValidateGateways("profiles."+profileName, ctx, cfg.NeverAllowEnv); err != nil {
					return err
				}
			}
			if err := config.ValidateGatewayTarget(name, gw); err != nil {
				return err
			}
			if token != "" && !force {
				if err := checkKeyUnclaimed(cfg, gw.Store, gw.Key); err != nil {
					return err
				}
			}

			// The new gateway is written in its one-API-per-gateway form: one
			// that serves Codex gets a Responses sibling.
			converted, _ := config.ConvertLegacyGateways(config.Context{Gateways: ctx.Gateways, DefaultCredentials: []string{name}})
			var added []string
			for _, k := range slices.Sorted(maps.Keys(converted)) {
				if _, saved := cfg.Contexts[profileName].Gateways[k]; k == name || !saved {
					ctx.Gateways[k] = converted[k].WithBaseURL()
					added = append(added, k)
				}
			}
			gw = ctx.Gateways[name]
			for _, h := range defaultFor {
				if !slices.ContainsFunc(added, func(k string) bool { return slices.Contains(ctx.Gateways[k].EffectiveHarnesses(), h) }) {
					return fmt.Errorf("--default-for %s: gateway %q does not serve %s", h, name, h)
				}
			}
			interactive := !tokenStdin && stdinIsTTY()
			wantDefault := func(k, h string) bool {
				switch {
				case makeDefault || slices.Contains(defaultFor, h):
					return true
				case cmd.Flags().Changed("default") || cmd.Flags().Changed("default-for") || !interactive || !isSlotHarness(h):
					return false
				}
				return promptDefaultFor(cmd, reader, k, h, profileName)
			}
			for _, k := range added {
				ctx = setGatewayDefaults(ctx, k, wantDefault)
			}

			home := userHomeDir()
			secretsDir := config.SecretsDir()
			restoreSecret := func() error { return nil }
			if token != "" && ageKey == "" {
				if _, statErr := os.Stat(config.ResolveSecretPath(gw.Store)); errors.Is(statErr, os.ErrNotExist) {
					if ageKey, err = secrets.ResolveRecipient("", reader, cmd.ErrOrStderr(), interactive, generateAgeKey); err != nil {
						return err
					}
				}
			}
			if token != "" {
				restore, err := secrets.NewManager(secretsDir, "").ReplaceKey(gw.Store, secretsDir, gw.Key, token, ageKey)
				if err != nil {
					return fmt.Errorf("storing the gateway token: %w", err)
				}
				restoreSecret = restore
				fmt.Fprintf(out, "✓ Encrypted token into %s (key: %s)\n",
					homepath.Tilde(config.ResolveSecretPath(gw.Store), home), gw.Key)
			}

			cfg.Contexts[profileName] = ctx
			if err := saveConfigWithMigrationPrompt(cmd, cfg); err != nil {
				return errors.Join(fmt.Errorf("writing config: %w", err), restoreErr(restoreSecret()))
			}
			if force && hadOld && old.RequiresToken() && !gw.RequiresToken() && secretUser(cfg, old.Store, old.Key) == "" {
				_ = secrets.NewManager(secretsDir, "").DeleteKey(old.Store, secretsDir, old.Key)
			}
			fmt.Fprintf(out, "✓ Saved gateway %q under profile %q in %s\n", name, profileName, homepath.Tilde(config.FilePath(), home))

			res, err := gateway.Sync(context.Background(), gateway.SyncOptions{
				ProfileName: profileName, Profile: ctx, GatewayName: name, Gateway: gw,
				Token: token, HomeDir: home, Progress: out, Prefetch: true, Models: models,
			})
			if err != nil {
				return fmt.Errorf("gateway %q is saved, but writing harness files failed (retry with: aide gateway sync %s --profile %s): %w",
					name, name, profileName, err)
			}
			printHarnessStatuses(out, res.HarnessStatuses, home)
			return nil
		},
	}
	cmd.Flags().StringVar(&rawURL, "url", "", "Gateway base URL (https://, or http:// for localhost only)")
	cmd.Flags().StringVar(&api, "api", "", "Wire protocol of a custom gateway (anthropic-messages)")
	cmd.Flags().BoolVar(&noAuth, "no-auth", false, "The gateway takes no key (loopback URLs only)")
	cmd.Flags().BoolVar(&tokenStdin, "key-stdin", false, "Read the key from stdin")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Deprecated: use --key-stdin")
	cmd.Flags().StringVar(&fromEnv, "from-env", "", "Read the key from this environment variable (bare: the preset's usual variable)")
	cmd.Flags().Lookup("from-env").NoOptDefVal = fromEnvAuto
	cmd.Flags().StringVar(&fromEnv, "token-env", "", "Deprecated: use --from-env=VAR")
	cmd.Flags().StringVar(&ttl, "refresh-ttl", "24h", "Launch-time model refresh interval (e.g. 12h, 24h, 168h)")
	cmd.Flags().StringVar(&ttl, "ttl", "24h", "Deprecated: use --refresh-ttl")
	cmd.Flags().StringVar(&harnesses, "harnesses", "", "Comma-separated harnesses to configure (default: the preset's, else claude,pi,omp,opencode,codex)")
	cmd.Flags().StringVar(&model, "model", "", "Model the harnesses start on (default: none)")
	cmd.Flags().StringVar(&store, "secret-store", "", "Secret store for the key (default: the profile's secret store, else the profile name)")
	cmd.Flags().StringVar(&store, "store", "", "Deprecated: use --secret-store")
	cmd.Flags().StringVar(&key, "key", "", "Key inside the store (default: <name>_token)")
	cmd.Flags().StringVar(&ageKey, "age-key", "", "age1... recipient for a new secret store (default: the discovered age identity)")
	cmd.Flags().BoolVar(&generateAgeKey, "generate-age-key", false, "Create an age identity when none exists, even without a terminal")
	cmd.Flags().StringSliceVar(&defaultFor, "default-for", nil, "Make this gateway the choice for these harnesses (repeatable)")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "Make this gateway the choice for every harness it serves")
	cmd.Flags().BoolVar(&force, "force", false, "Replace a gateway of the same name with another URL, or overwrite an existing secret key")
	for old, use := range map[string]string{"token-stdin": "--key-stdin", "token-env": "--from-env=VAR", "ttl": "--refresh-ttl", "store": "--secret-store"} {
		_ = cmd.Flags().MarkDeprecated(old, "use "+use)
	}
	return cmd
}

// fromEnvAuto is --from-env given without a variable name.
const fromEnvAuto = "\x00auto"

// errWireNotDelivered is why a wire protocol other than Anthropic
// Messages is refused: the harness writers only emit that one.
var errWireNotDelivered = errors.New("aide cannot yet write this wire protocol into harness files; only anthropic-messages gateways with a Bearer key are supported")

// presetDeliverable refuses a preset whose wire protocol, key header or
// kind the harness writers cannot deliver yet.
func presetDeliverable(p gatewaypreset.Preset) error {
	if p.Kind != gatewaypreset.KindStaticKey || p.API != gatewaypreset.APIAnthropicMessages || p.Header != gatewaypreset.HeaderBearer {
		return fmt.Errorf("preset %s (%s, %s): %w", p.Name, p.Kind, cmp.Or(string(p.API), "no wire protocol"), errWireNotDelivered)
	}
	return nil
}

// presetShellVar resolves a bare --from-env: the first of the preset's
// usual variables that is set.
func presetShellVar(p gatewaypreset.Preset, isPreset bool) (string, error) {
	if !isPreset {
		return "", errors.New("--from-env needs a variable name for a custom gateway: use --from-env=VAR, not --from-env VAR")
	}
	for _, v := range p.ShellVars {
		if os.Getenv(v) != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("none of %s is set; export the key or use --from-env=VAR", strings.Join(p.ShellVars, ", "))
}

func isSlotHarness(h string) bool {
	d, ok := harness.Lookup(h)
	return ok && d.Reach == harness.ReachSlot
}

func promptDefaultFor(cmd *cobra.Command, reader *bufio.Reader, name, h, profileName string) bool {
	fmt.Fprintf(cmd.ErrOrStderr(), "Make %s the default for %s in profile %s? [y/N]: ", name, h, profileName)
	line, _ := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// setGatewayDefaults records gateway_choice for the harnesses name
// serves: name where want says so; otherwise none for a claude or
// codex whose choice is unset and not already covered by a provider
// credential default, so launches stay on the harness's own login.
// Multi harnesses and existing choices are left alone.
func setGatewayDefaults(ctx config.Context, name string, want func(name, h string) bool) config.Context {
	choice := maps.Clone(ctx.GatewayChoice)
	if choice == nil {
		choice = map[string]string{}
	}
	for _, h := range ctx.Gateways[name].EffectiveHarnesses() {
		switch {
		case want(name, h):
			choice[h] = name
		case isSlotHarness(h) && choice[h] == "" && !slices.ContainsFunc(config.DefaultCredentialsForHarness(ctx, h), func(d string) bool { return ctx.Credentials[d].IsLLMProvider(d) }):
			choice[h] = "none"
		}
	}
	ctx.GatewayChoice = choice
	return ctx
}

func gatewayListCmd(profile *string) *cobra.Command {
	var (
		all    bool
		models bool
	)
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "Show configured gateways, cache freshness, and harness file status",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.FromCmd(cmd)
			if err != nil {
				return err
			}
			if all && *profile != "" {
				return errors.New("--profile and --all cannot be used together")
			}
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg := env.Config()
			var profiles []string
			if all {
				profiles = slices.Sorted(maps.Keys(cfg.Contexts))
			} else {
				p, _, err := resolveGatewayProfile(cfg, env.CWD(), *profile)
				if err != nil {
					if *profile == "" {
						return fmt.Errorf("no profile matches this directory (pass --profile <name> or --all): %w", err)
					}
					return err
				}
				profiles = []string{p}
			}
			home := userHomeDir()
			entries := []GatewayListEntry{}
			for _, p := range profiles {
				ctx := cfg.Contexts[p]
				var pEntries []GatewayListEntry
				for _, name := range slices.Sorted(maps.Keys(ctx.Credentials)) {
					b := ctx.Credentials[name]
					if !b.IsLLMProvider(name) {
						continue
					}
					pEntries = append(pEntries, credentialListEntry(p, ctx, name, b))
				}
				for _, name := range slices.Sorted(maps.Keys(ctx.Gateways)) {
					pEntries = append(pEntries, gatewayListEntry(p, ctx, name, home, models))
				}
				chosen := chosenFor(cfg, p, ctx)
				for i := range pEntries {
					pEntries[i].ChosenFor = append([]GatewayChoice{}, chosen[pEntries[i].Gateway]...)
				}
				slices.SortStableFunc(pEntries, func(a, b GatewayListEntry) int {
					aActive, bActive := len(a.DefaultFor) > 0, len(b.DefaultFor) > 0
					if aActive != bActive {
						if aActive {
							return -1
						}
						return 1
					}
					return cmp.Compare(a.Gateway, b.Gateway)
				})
				entries = append(entries, pEntries...)
			}
			return output.Emit(cmd.OutOrStdout(), format, entries, func(w io.Writer) error {
				printGatewayList(w, entries, cfg, home, models)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Show gateways across all profiles")
	cmd.Flags().BoolVar(&models, "models", false, "Include each gateway's cached models")
	return cmd
}

var gatewayCredentialHarnesses = []string{"claude", "pi", "omp", "opencode", "codex", "gemini"}

func gatewayUseCmd(profile *string) *cobra.Command {
	var harnessName string
	var repo, unset bool
	cmd := &cobra.Command{
		Use:   "use [<name|none>] --harness <h> [--repo] [--unset]",
		Short: "Choose the gateway a harness uses",
		Long: `Set which gateway a harness uses: a gateway marked for it, or none for the
harness's own login. The choice is saved in the profile's gateway_choice,
or with --repo for this repository only. --unset removes the saved choice.

Without <name> on a terminal, lists the gateways marked for the harness
and prompts, with nothing preselected.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if harnessName == "" {
				return errors.New("--harness is required (one of " + strings.Join(config.DefaultGatewayHarnesses, ", ") + ")")
			}
			if !slices.Contains(config.DefaultGatewayHarnesses, harnessName) {
				return fmt.Errorf("--harness %q is not a gateway harness (use %s)", harnessName, strings.Join(config.DefaultGatewayHarnesses, ", "))
			}
			if unset && len(args) == 1 {
				return errors.New("--unset takes no gateway name")
			}
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg, err := env.MigratedConfig()
			if err != nil {
				return err
			}
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), *profile)
			if err != nil {
				return err
			}
			var candidates []string
			for _, name := range slices.Sorted(maps.Keys(ctx.Gateways)) {
				if slices.Contains(ctx.Gateways[name].EffectiveHarnesses(), harnessName) {
					candidates = append(candidates, name)
				}
			}
			name := ""
			switch {
			case unset:
			case len(args) == 1:
				name = args[0]
			case !stdinIsTTY():
				return errors.New("gateway name (or none) is required when stdin is not a terminal")
			default:
				if name, err = promptGatewayUse(cmd, bufio.NewReader(cmd.InOrStdin()), profileName, harnessName, candidates); err != nil {
					return err
				}
			}
			if name != "" && name != "none" && !slices.Contains(candidates, name) {
				hint := ""
				if _, isGW := ctx.Gateways[name]; !isGW {
					hint = "; provider credentials cannot be chosen here until they migrate: launch with --gateway " + name + " or run aide project set --gateway " + name + " --harness " + harnessName
				}
				return fmt.Errorf("profile %q has no gateway %q marked for %s (marked: %s)%s",
					profileName, name, harnessName, strings.Join(append(candidates, "none"), ", "), hint)
			}

			if repo {
				return saveRepoGatewayChoice(cmd, profileName, env.CWD(), harnessName, name)
			}
			ctx.GatewayChoice = maps.Clone(ctx.GatewayChoice)
			if name == "" {
				delete(ctx.GatewayChoice, harnessName)
			} else {
				if ctx.GatewayChoice == nil {
					ctx.GatewayChoice = map[string]string{}
				}
				ctx.GatewayChoice[harnessName] = name
			}
			cfg.Contexts[profileName] = ctx
			if err := saveConfigWithMigrationPrompt(cmd, cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			if name == "" {
				fmt.Fprintf(out, "✓ Unset the gateway choice for %s on profile %q\n", harnessName, profileName)
				return nil
			}
			fmt.Fprintf(out, "✓ Set the gateway for %s on profile %q to %q\n", harnessName, profileName, name)
			if gw, isGw := ctx.Gateways[name]; isGw {
				home := userHomeDir()
				if cat, err := gateway.ReadCache(home, profileName, name); err == nil && gateway.MatchesBaseURL(cat, gw.BaseURL) {
					materializeGatewayFromCache(out, ctx, name, gw, cat, home)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&harnessName, "harness", "", "Harness the choice is for (claude, codex, pi, omp or opencode)")
	cmd.Flags().BoolVar(&repo, "repo", false, "Save for this repository only, not in the profile")
	cmd.Flags().BoolVar(&unset, "unset", false, "Remove the saved choice")
	return cmd
}

// promptGatewayUse asks which of candidates (or none) harnessName
// should use. Nothing is preselected: an empty answer is an error.
func promptGatewayUse(cmd *cobra.Command, reader *bufio.Reader, profileName, harnessName string, candidates []string) (string, error) {
	options := append(slices.Clone(candidates), "none")
	fmt.Fprintf(cmd.ErrOrStderr(), "Gateways for %s on profile %q:\n", harnessName, profileName)
	for i, o := range options {
		fmt.Fprintf(cmd.ErrOrStderr(), "  [%d] %s\n", i+1, o)
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Select: ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading selection: %w", err)
	}
	ans := strings.TrimSpace(line)
	if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(options) {
		return options[idx-1], nil
	}
	if ans == "" {
		return "", errors.New("no gateway selected")
	}
	return ans, nil
}

// saveRepoGatewayChoice saves (or, for an empty name, removes) the
// repository's gateway choice for harnessName.
func saveRepoGatewayChoice(cmd *cobra.Command, profileName, cwd, harnessName, name string) error {
	key := launcher.PreferenceKey(profileName, aidectx.GitRemotes(cwd))
	if err := key.Validate(); err != nil {
		return fmt.Errorf("cannot save repository preferences: %w", err)
	}
	path := prefs.DefaultPath(userHomeDir())
	err := prefs.Update(path, func(f *prefs.File) error {
		p, _ := f.Get(key)
		p.Gateway = maps.Clone(p.Gateway)
		if name == "" {
			delete(p.Gateway, harnessName)
		} else {
			if p.Gateway == nil {
				p.Gateway = map[string]string{}
			}
			p.Gateway[harnessName] = name
		}
		f.Set(key, p)
		return nil
	})
	if err != nil {
		return err
	}
	if name == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Unset the repository's gateway choice for %s under profile %q\n", harnessName, profileName)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Set the repository's gateway for %s under profile %q to %q\n", harnessName, profileName, name)
	return nil
}

func gatewayEditCmd(profile *string) *cobra.Command {
	var (
		flagName, rawURL, ttl, harnesses, store, key, ageKey, tokenEnv string
		rotateToken, tokenStdin, force                                 bool
	)
	cmd := &cobra.Command{
		Use:   "edit [<name>]",
		Short: "Update a gateway's name, URL, token, TTL, or target harnesses",
		Long: `Update an existing custom gateway on a profile in place.

Pass flags (--name, --url, --ttl, --harnesses, --rotate-token, --token-stdin,
--token-env, --store, --key) to update specific fields, or run interactively
on a terminal with no flags to review and edit each field with its current
value as the default.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokenStdin && tokenEnv != "" {
				return errors.New("--token-stdin and --token-env are mutually exclusive")
			}
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(cmd.InOrStdin())
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg, err := env.MigratedConfig()
			if err != nil {
				return err
			}
			profileName, oldName, ctx, err := resolveEditableGatewayTarget(cmd, reader, cfg, env.CWD(), *profile, args, tokenStdin)
			if err != nil {
				return err
			}
			oldGW := ctx.Gateways[oldName]

			hasFieldFlags := cmd.Flags().Changed("name") ||
				cmd.Flags().Changed("url") ||
				cmd.Flags().Changed("ttl") ||
				cmd.Flags().Changed("harnesses") ||
				cmd.Flags().Changed("store") ||
				cmd.Flags().Changed("key")
			hasNonInteractiveToken := tokenStdin || tokenEnv != ""

			newName := oldName
			newGW := oldGW
			wantTokenRotate := rotateToken || hasNonInteractiveToken

			if hasFieldFlags || hasNonInteractiveToken {
				if cmd.Flags().Changed("name") {
					newName = strings.TrimSpace(flagName)
				}
				if cmd.Flags().Changed("url") {
					newGW.BaseURL = gateway.NormalizeBaseURL(rawURL)
				}
				if cmd.Flags().Changed("ttl") {
					newGW.RefreshTTL = strings.TrimSpace(ttl)
				}
				if cmd.Flags().Changed("harnesses") {
					newGW.Harnesses = parseGatewayHarnesses(harnesses)
				}
				if cmd.Flags().Changed("store") {
					newGW.Store = strings.TrimSpace(store)
				}
				if cmd.Flags().Changed("key") {
					newGW.Key = strings.TrimSpace(key)
				}
				urlChanged := gateway.NormalizeBaseURL(newGW.BaseURL) != gateway.NormalizeBaseURL(oldGW.BaseURL)
				willPromptTTY := (rotateToken && !hasNonInteractiveToken) || (!wantTokenRotate && cmd.Flags().Changed("url") && urlChanged && stdinIsTTY())
				if willPromptTTY {
					fmt.Fprintf(cmd.ErrOrStderr(), "Editing gateway %q on profile %q\n", oldName, profileName)
				}
				if !wantTokenRotate && cmd.Flags().Changed("url") && urlChanged && stdinIsTTY() {
					wantTokenRotate = promptRotateToken(cmd, reader)
				}
			} else {
				if !stdinIsTTY() {
					return errors.New("no changes specified; pass at least one flag (--name, --url, --ttl, --harnesses, --store, --key, --token-stdin, --token-env) or run on a terminal")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Editing gateway %q on profile %q\n", oldName, profileName)
				var err error
				newName, err = promptFieldDefault(cmd, reader, "Gateway name", oldName)
				if err != nil {
					return err
				}
				editedURL, err := promptFieldDefault(cmd, reader, "Gateway base URL", oldGW.BaseURL)
				if err != nil {
					return err
				}
				newGW.BaseURL = gateway.NormalizeBaseURL(editedURL)
				editedTTL, err := promptFieldDefault(cmd, reader, "Refresh TTL", cmp.Or(oldGW.RefreshTTL, "24h"))
				if err != nil {
					return err
				}
				newGW.RefreshTTL = editedTTL
				editedHarnesses, err := promptFieldDefault(cmd, reader, "Harnesses", strings.Join(oldGW.EffectiveHarnesses(), ","))
				if err != nil {
					return err
				}
				newGW.Harnesses = parseGatewayHarnesses(editedHarnesses)
				if rotateToken {
					wantTokenRotate = true
				} else {
					wantTokenRotate = promptRotateToken(cmd, reader)
				}
			}

			if newName != oldName && !cmd.Flags().Changed("key") && oldGW.Key == defaultGatewayKey(oldName) {
				newGW.Key = defaultGatewayKey(newName)
			}

			if !wantTokenRotate && newName == oldName &&
				gateway.NormalizeBaseURL(newGW.BaseURL) == gateway.NormalizeBaseURL(oldGW.BaseURL) &&
				cmp.Or(newGW.RefreshTTL, "24h") == cmp.Or(oldGW.RefreshTTL, "24h") &&
				newGW.Store == oldGW.Store &&
				newGW.Key == oldGW.Key &&
				slices.Equal(newGW.EffectiveHarnesses(), oldGW.EffectiveHarnesses()) {
				fmt.Fprintf(out, "No changes to gateway %q\n", oldName)
				return nil
			}

			var newToken string
			if wantTokenRotate {
				newToken, err = readGatewayToken(cmd, reader, tokenStdin, tokenEnv, false)
				if err != nil {
					return err
				}
				if !oldGW.RequiresToken() {
					if newGW.Store == "" {
						newGW.Store = ctx.Secret
					}
					if newGW.Store == "" {
						newGW.Store = profileName
					}
					if newGW.Key == "" {
						newGW.Key = defaultGatewayKey(newName)
					}
				}
			}

			return applyGatewayEdit(cmd.Context(), out, cmd.ErrOrStderr(), cfg, profileName, oldName, newName, oldGW, newGW, newToken, ageKey, wantTokenRotate, force)
		},
	}
	cmd.Flags().StringVar(&flagName, "name", "", "Rename the gateway to this name")
	cmd.Flags().StringVar(&rawURL, "url", "", "Updated gateway base URL (https://, or http:// for localhost only)")
	cmd.Flags().StringVar(&ttl, "ttl", "", "Updated launch-time model refresh interval (e.g. 12h, 24h, 168h)")
	cmd.Flags().StringVar(&harnesses, "harnesses", "", "Comma-separated harnesses to configure, or 'all' (default: claude,pi,omp,opencode,codex)")
	cmd.Flags().BoolVar(&rotateToken, "rotate-token", false, "Prompt for a new bearer token and re-sync models")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read a new bearer token from stdin")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "Read a new bearer token from this environment variable")
	cmd.Flags().StringVar(&store, "store", "", "Move the token to this secret store")
	cmd.Flags().StringVar(&key, "key", "", "Secret key name inside the store")
	cmd.Flags().StringVar(&ageKey, "age-key", "", "age1... recipient for a new secret store")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing unclaimed secret key in the target store")
	return cmd
}

func gatewaySyncCmd(profile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [<name>]",
		Short: "Force-refresh gateway models and update harness config files",
		Long: `Fetch each gateway's /v1/models now, whatever its refresh_ttl, update the
cached catalogue, and write it into every provisioned harness config.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			profileName, ctx, err := resolveGatewayProfile(env.Config(), env.CWD(), *profile)
			if err != nil {
				return err
			}
			names := slices.Sorted(maps.Keys(ctx.Gateways))
			if len(args) == 1 {
				if _, ok := ctx.Gateways[args[0]]; !ok {
					return fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, args[0], definedGateways(ctx))
				}
				names = args[:1]
			}
			if len(names) == 0 {
				return fmt.Errorf("profile %q has no gateways; add one with: aide gateway add", profileName)
			}
			home := userHomeDir()
			load := launcher.CachingStoreLoader(secrets.LoadSecretsMap)
			var failed []string
			for _, name := range names {
				gw := ctx.Gateways[name]
				if err := config.ValidateGatewayTarget(name, gw); err != nil {
					fmt.Fprintf(out, "✗ %v\n", err)
					failed = append(failed, name)
					continue
				}
				fmt.Fprintf(out, "→ Syncing gateway %q (profile %q)\n", name, profileName)
				token := ""
				var err error
				if gw.RequiresToken() {
					token, err = launcher.ResolveStoreKey(load, fmt.Sprintf("gateway %q", name), gw.Store, gw.Key)
				}
				if err == nil {
					var res *gateway.SyncResult
					res, err = gateway.Sync(context.Background(), gateway.SyncOptions{
						ProfileName: profileName, Profile: ctx, GatewayName: name, Gateway: gw,
						Token: token, HomeDir: home, Progress: out,
					})
					if err == nil {
						printHarnessStatuses(out, res.HarnessStatuses, home)
						continue
					}
				}
				fmt.Fprintf(out, "✗ %v\n", err)
				failed = append(failed, name)
			}
			if len(failed) > 0 {
				return fmt.Errorf("could not sync gateway(s): %s", strings.Join(failed, ", "))
			}
			return nil
		},
	}
	return cmd
}

func gatewayRemoveCmd(profile *string) *cobra.Command {
	var keepSecret bool
	cmd := &cobra.Command{
		Use:          "remove <name>",
		Short:        "Remove a gateway, its cached models, and harness provider entries",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			name := args[0]
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg, err := env.MigratedConfig()
			if err != nil {
				return err
			}
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), *profile)
			if err != nil {
				return err
			}
			gw, ok := ctx.Gateways[name]
			if !ok {
				return fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, name, definedGateways(ctx))
			}
			home := userHomeDir()

			updated := ctx
			updated.Gateways = maps.Clone(ctx.Gateways)
			delete(updated.Gateways, name)
			if len(updated.Gateways) == 0 {
				updated.Gateways = nil
			}
			updated.GatewayChoice = maps.Clone(ctx.GatewayChoice)
			maps.DeleteFunc(updated.GatewayChoice, func(_, v string) bool { return v == name })
			cfg.Contexts[profileName] = updated
			if err := saveConfigWithMigrationPrompt(cmd, cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "✓ Removed gateway %q from profile %q in %s\n", name, profileName, homepath.Tilde(config.FilePath(), home))

			if gw.RequiresToken() {
				storePath := homepath.Tilde(config.ResolveSecretPath(gw.Store), home)
				switch user := secretUser(cfg, gw.Store, gw.Key); {
				case keepSecret:
					fmt.Fprintf(out, "- Kept secret key %q in %s (--keep-secret)\n", gw.Key, storePath)
				case user != "":
					fmt.Fprintf(out, "- Kept secret key %q in %s (still used by %s)\n", gw.Key, storePath, user)
				default:
					secretsDir := config.SecretsDir()
					if err := secrets.NewManager(secretsDir, "").DeleteKey(gw.Store, secretsDir, gw.Key); err != nil {
						return fmt.Errorf("deleting secret key %q: %w", gw.Key, err)
					}
					fmt.Fprintf(out, "✓ Deleted secret key %q from %s\n", gw.Key, storePath)
				}
			}

			if err := gateway.RemoveCache(home, profileName, name); err != nil {
				return err
			}
			cachePath, _ := gateway.CachePath(home, profileName, name)
			fmt.Fprintf(out, "✓ Removed cache %s\n", homepath.Tilde(cachePath, home))

			for _, h := range gw.EffectiveHarnesses() {
				dir, _ := gateway.HarnessConfigDir(ctx, h, home)
				removed, err := gateway.RemoveFromHarness(ctx, name, gw, h, home)
				switch {
				case err != nil:
					fmt.Fprintf(out, "  ✗ %-9s %v\n", h, err)
				case !removed:
					fmt.Fprintf(out, "  - %-9s skipped %s (no entry for %q)\n", h, homepath.Tilde(dir, home), name)
				case gateway.SingleGatewayPerDir(h):
					fmt.Fprintf(out, "  ✓ %-9s removed gateway model cache in %s\n", h, homepath.Tilde(dir, home))
				default:
					fmt.Fprintf(out, "  ✓ %-9s removed provider %q in %s\n", h, name, homepath.Tilde(dir, home))
				}
			}
			reconcileProfileHarnesses(updated, profileName, "", home)
			if err := updateRepoPrefsGatewayRemove(home, profileName, name); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "aide: warning: could not update repository preferences: %v\n", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepSecret, "keep-secret", false, "Keep the token in the secret store")
	return cmd
}

func defaultGatewayKey(name string) string {
	return strings.ReplaceAll(name, "-", "_") + "_token"
}

func parseGatewayHarnesses(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "all") {
		return nil
	}
	var list []string
	for _, h := range strings.Split(trimmed, ",") {
		if s := strings.TrimSpace(h); s != "" {
			list = append(list, s)
		}
	}
	if len(list) == len(config.DefaultGatewayHarnesses) {
		allDefault := true
		for _, d := range config.DefaultGatewayHarnesses {
			if !slices.Contains(list, d) {
				allDefault = false
				break
			}
		}
		if allDefault {
			return nil
		}
	}
	return list
}

type editableGatewayChoice struct {
	profile string
	gateway string
	baseURL string
}

func resolveEditableGatewayTarget(cmd *cobra.Command, reader *bufio.Reader, cfg *config.Config, cwd, profileFlag string, args []string, tokenStdin bool) (string, string, config.Context, error) {
	if profileFlag != "" {
		profileName, ctx, err := resolveGatewayProfile(cfg, cwd, profileFlag)
		if err != nil {
			return "", "", config.Context{}, err
		}
		if len(args) == 1 {
			name := args[0]
			if _, ok := ctx.Gateways[name]; !ok {
				return "", "", config.Context{}, fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, name, definedGateways(ctx))
			}
			return profileName, name, ctx, nil
		}
		names := slices.Sorted(maps.Keys(ctx.Gateways))
		if len(names) == 0 {
			return "", "", config.Context{}, fmt.Errorf("profile %q has no gateways; add one with: aide gateway add", profileName)
		}
		if tokenStdin || !stdinIsTTY() {
			return "", "", config.Context{}, errors.New("gateway name is required when stdin is not a terminal")
		}
		if len(names) == 1 {
			return profileName, names[0], ctx, nil
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Custom gateways on profile %q:\n", profileName)
		tw := tabwriter.NewWriter(cmd.ErrOrStderr(), 0, 0, 2, ' ', 0)
		for i, n := range names {
			fmt.Fprintf(tw, "  [%d] %s\t%s\n", i+1, n, ctx.Gateways[n].BaseURL)
		}
		_ = tw.Flush()
		fmt.Fprint(cmd.ErrOrStderr(), "Select gateway to edit [1]: ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", "", config.Context{}, fmt.Errorf("reading selection: %w", err)
		}
		ans := strings.TrimSpace(line)
		name := names[0]
		if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(names) {
			name = names[idx-1]
		} else if ans != "" {
			name = ans
		}
		if _, ok := ctx.Gateways[name]; !ok {
			return "", "", config.Context{}, fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, name, definedGateways(ctx))
		}
		return profileName, name, ctx, nil
	}

	cwdProfile := ""
	if rc, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd)); err == nil {
		cwdProfile = rc.Name
	}

	var choices []editableGatewayChoice
	appendProfileGateways := func(p string) {
		ctx := cfg.Contexts[p]
		for _, g := range slices.Sorted(maps.Keys(ctx.Gateways)) {
			choices = append(choices, editableGatewayChoice{
				profile: p,
				gateway: g,
				baseURL: ctx.Gateways[g].BaseURL,
			})
		}
	}
	if cwdProfile != "" {
		if _, ok := cfg.Contexts[cwdProfile]; ok {
			appendProfileGateways(cwdProfile)
		}
	}
	for _, p := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		if p == cwdProfile {
			continue
		}
		appendProfileGateways(p)
	}

	if len(args) == 1 {
		name := args[0]
		if cwdProfile != "" {
			if ctx, ok := cfg.Contexts[cwdProfile]; ok {
				if _, has := ctx.Gateways[name]; has {
					return cwdProfile, name, ctx, nil
				}
			}
		}
		var matches []string
		for _, c := range choices {
			if c.gateway == name {
				matches = append(matches, c.profile)
			}
		}
		switch len(matches) {
		case 1:
			p := matches[0]
			return p, name, cfg.Contexts[p], nil
		case 0:
			if len(choices) == 0 {
				return "", "", config.Context{}, errors.New("no gateways configured on any profile; add one with: aide gateway add")
			}
			var configured []string
			for _, c := range choices {
				configured = append(configured, c.profile+"/"+c.gateway)
			}
			return "", "", config.Context{}, fmt.Errorf("gateway %q not found on any profile (configured: %s)", name, strings.Join(configured, ", "))
		default:
			if tokenStdin || !stdinIsTTY() {
				return "", "", config.Context{}, fmt.Errorf("gateway %q exists on multiple profiles (%s); pass --profile <name>", name, strings.Join(matches, ", "))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Gateway %q exists on multiple profiles:\n", name)
			tw := tabwriter.NewWriter(cmd.ErrOrStderr(), 0, 0, 2, ' ', 0)
			for i, p := range matches {
				fmt.Fprintf(tw, "  [%d] %s\t%s\n", i+1, p, cfg.Contexts[p].Gateways[name].BaseURL)
			}
			_ = tw.Flush()
			fmt.Fprint(cmd.ErrOrStderr(), "Select profile to edit [1]: ")
			line, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return "", "", config.Context{}, fmt.Errorf("reading selection: %w", err)
			}
			ans := strings.TrimSpace(line)
			chosen := matches[0]
			if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(matches) {
				chosen = matches[idx-1]
			} else if ans != "" {
				chosen = ans
			}
			if !slices.Contains(matches, chosen) {
				return "", "", config.Context{}, fmt.Errorf("profile %q does not have gateway %q (matches: %s)", chosen, name, strings.Join(matches, ", "))
			}
			return chosen, name, cfg.Contexts[chosen], nil
		}
	}

	if len(choices) == 0 {
		return "", "", config.Context{}, errors.New("no gateways configured on any profile; add one with: aide gateway add")
	}
	if tokenStdin || !stdinIsTTY() {
		return "", "", config.Context{}, errors.New("gateway name is required when stdin is not a terminal")
	}
	if len(choices) == 1 {
		c := choices[0]
		return c.profile, c.gateway, cfg.Contexts[c.profile], nil
	}

	fmt.Fprintln(cmd.ErrOrStderr(), "Configured gateways:")
	tw := tabwriter.NewWriter(cmd.ErrOrStderr(), 0, 0, 2, ' ', 0)
	for i, c := range choices {
		fmt.Fprintf(tw, "  [%d] %s / %s\t%s\n", i+1, c.profile, c.gateway, c.baseURL)
	}
	_ = tw.Flush()
	fmt.Fprint(cmd.ErrOrStderr(), "Select gateway to edit [1]: ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", config.Context{}, fmt.Errorf("reading selection: %w", err)
	}
	ans := strings.TrimSpace(line)
	if ans == "" {
		c := choices[0]
		return c.profile, c.gateway, cfg.Contexts[c.profile], nil
	}
	if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(choices) {
		c := choices[idx-1]
		return c.profile, c.gateway, cfg.Contexts[c.profile], nil
	}
	if p, g, ok := strings.Cut(ans, "/"); ok {
		p, g = strings.TrimSpace(p), strings.TrimSpace(g)
		if ctx, exists := cfg.Contexts[p]; exists {
			if _, has := ctx.Gateways[g]; has {
				return p, g, ctx, nil
			}
		}
	}
	var matches []editableGatewayChoice
	for _, c := range choices {
		if c.gateway == ans {
			matches = append(matches, c)
		}
	}
	if len(matches) == 1 {
		c := matches[0]
		return c.profile, c.gateway, cfg.Contexts[c.profile], nil
	}
	if len(matches) > 1 {
		var profs []string
		for _, m := range matches {
			profs = append(profs, m.profile)
		}
		return "", "", config.Context{}, fmt.Errorf("gateway %q exists on multiple profiles (%s); select by number or profile/gateway", ans, strings.Join(profs, ", "))
	}
	var configured []string
	for _, c := range choices {
		configured = append(configured, c.profile+"/"+c.gateway)
	}
	return "", "", config.Context{}, fmt.Errorf("gateway %q not found on any profile (configured: %s)", ans, strings.Join(configured, ", "))
}

func promptFieldDefault(cmd *cobra.Command, reader *bufio.Reader, label, current string) (string, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [%s]: ", label, current)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading %s: %w", strings.ToLower(label), err)
	}
	val := strings.TrimSpace(line)
	if val == "" {
		return current, nil
	}
	return val, nil
}

func promptRotateToken(cmd *cobra.Command, reader *bufio.Reader) bool {
	fmt.Fprint(cmd.ErrOrStderr(), "Rotate bearer token? [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil && errors.Is(err, io.EOF) && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// restoreErr labels a failed secret rollback so the user knows the store
// may hold the new token; nil stays nil for errors.Join.
func restoreErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restoring the previous gateway token: %w", err)
}

func applyGatewayEdit(syncCtx context.Context, out, errOut io.Writer, cfg *config.Config, profileName, oldName, newName string, oldGW, newGW config.GatewayConfig, newToken, ageKey string, tokenChanged, force bool) error {
	ctx := cfg.Contexts[profileName]
	if newName != oldName {
		if !config.IsGatewayName(newName) {
			return fmt.Errorf("invalid gateway name %q (must match [a-zA-Z0-9][a-zA-Z0-9_-]*)", newName)
		}
		if existing, exists := ctx.Gateways[newName]; exists {
			return fmt.Errorf("profile %q already has gateway %q for %s", profileName, newName, existing.BaseURL)
		}
		if _, exists := ctx.Credentials[newName]; exists {
			return fmt.Errorf("profile %q already has credential %q", profileName, newName)
		}
	}

	updatedCtx := ctx
	updatedCtx.Gateways = maps.Clone(ctx.Gateways)
	delete(updatedCtx.Gateways, oldName)
	updatedCtx.Gateways[newName] = newGW
	if newName != oldName {
		updatedCtx.GatewayChoice = maps.Clone(ctx.GatewayChoice)
		for h, v := range updatedCtx.GatewayChoice {
			if v == oldName {
				updatedCtx.GatewayChoice[h] = newName
			}
		}
	}
	if err := config.ValidateGateways("profiles."+profileName, updatedCtx, cfg.NeverAllowEnv); err != nil {
		return err
	}

	if err := config.ValidateGatewayTarget(newName, newGW); err != nil {
		return err
	}

	keyOrStoreChanged := newGW.Store != oldGW.Store || newGW.Key != oldGW.Key
	urlChanged := gateway.NormalizeBaseURL(newGW.BaseURL) != gateway.NormalizeBaseURL(oldGW.BaseURL)
	needsLiveSync := tokenChanged || urlChanged

	if keyOrStoreChanged && newGW.RequiresToken() && !force {
		if err := checkKeyUnclaimed(cfg, newGW.Store, newGW.Key); err != nil {
			return err
		}
	}

	activeToken := newToken
	if !tokenChanged && (keyOrStoreChanged || needsLiveSync) && oldGW.RequiresToken() {
		load := launcher.CachingStoreLoader(secrets.LoadSecretsMap)
		var err error
		activeToken, err = launcher.ResolveStoreKey(load, fmt.Sprintf("gateway %q", oldName), oldGW.Store, oldGW.Key)
		if err != nil {
			return err
		}
	}

	home := userHomeDir()
	var verifiedCat *gateway.CachedCatalogue
	if needsLiveSync {
		if syncCtx == nil {
			syncCtx = context.Background()
		}
		base := gateway.NormalizeBaseURL(newGW.BaseURL)
		ep := gateway.Endpoint(newGW)
		fmt.Fprintf(out, "→ Fetching models from %s...\n", ep.ModelsURL())
		fctx, cancel := context.WithTimeout(syncCtx, gateway.DefaultFetchTimeout)
		models, err := gateway.FetchModels(fctx, nil, ep, activeToken)
		cancel()
		if err != nil {
			return fmt.Errorf("verifying gateway %q at %s: %w", newName, base, err)
		}
		verifiedCat = &gateway.CachedCatalogue{
			Profile:   profileName,
			Gateway:   newName,
			BaseURL:   base,
			FetchedAt: time.Now().UTC(),
			Models:    models,
		}
	}

	secretsDir := config.SecretsDir()
	restoreSecret := func() error { return nil }
	if (tokenChanged || keyOrStoreChanged) && newGW.RequiresToken() {
		restore, err := secrets.NewManager(secretsDir, "").ReplaceKey(newGW.Store, secretsDir, newGW.Key, activeToken, ageKey)
		if err != nil {
			return fmt.Errorf("storing the gateway token: %w", err)
		}
		restoreSecret = restore
		if keyOrStoreChanged && !tokenChanged {
			fmt.Fprintf(out, "✓ Migrated secret key %q → %q in %s\n",
				oldGW.Key, newGW.Key, homepath.Tilde(config.ResolveSecretPath(newGW.Store), home))
		} else {
			fmt.Fprintf(out, "✓ Encrypted token into %s (key: %s)\n",
				homepath.Tilde(config.ResolveSecretPath(newGW.Store), home), newGW.Key)
		}
	}

	cfg.Contexts[profileName] = updatedCtx
	if err := saveConfigWithMigrationPrompt(nil, cfg); err != nil {
		return errors.Join(fmt.Errorf("writing config: %w", err), restoreErr(restoreSecret()))
	}
	if newName != oldName {
		fmt.Fprintf(out, "✓ Renamed gateway %q → %q under profile %q in %s\n",
			oldName, newName, profileName, homepath.Tilde(config.FilePath(), home))
	} else {
		fmt.Fprintf(out, "✓ Updated gateway %q under profile %q in %s\n",
			newName, profileName, homepath.Tilde(config.FilePath(), home))
	}

	if keyOrStoreChanged && secretUser(cfg, oldGW.Store, oldGW.Key) == "" {
		_ = secrets.NewManager(secretsDir, "").DeleteKey(oldGW.Store, secretsDir, oldGW.Key)
	}

	if newName != oldName {
		if err := updateRepoPrefsGatewayName(home, profileName, oldName, newName); err != nil {
			fmt.Fprintf(errOut, "aide: warning: could not update repository preferences: %v\n", err)
		}
	}

	oldCat, _ := gateway.ReadCache(home, profileName, oldName)
	if newName != oldName {
		for _, h := range oldGW.EffectiveHarnesses() {
			_, _ = gateway.RemoveFromHarness(ctx, oldName, oldGW, h, home)
		}
		_ = gateway.RemoveCache(home, profileName, oldName)
	} else {
		newSet := newGW.EffectiveHarnesses()
		for _, h := range oldGW.EffectiveHarnesses() {
			if !slices.Contains(newSet, h) || (h == "claude" && !gateway.CanSyncHarness(updatedCtx, newName, "claude")) {
				_, _ = gateway.RemoveFromHarness(ctx, oldName, oldGW, h, home)
			}
		}
	}

	if verifiedCat != nil {
		if err := gateway.WriteCache(home, verifiedCat); err != nil {
			return err
		}
		cachePath, _ := gateway.CachePath(home, profileName, newName)
		fmt.Fprintf(out, "✓ Cached %d LLM models -> %s\n", len(verifiedCat.Models), homepath.Tilde(cachePath, home))
		materializeGatewayFromCache(out, updatedCtx, newName, newGW, verifiedCat, home)
	} else if gateway.MatchesBaseURL(oldCat, newGW.BaseURL) {
		oldCachePath, _ := gateway.CachePath(home, profileName, oldName)
		oldCat.Gateway = newName
		oldCat.BaseURL = newGW.BaseURL
		if err := gateway.WriteCache(home, oldCat); err != nil {
			return err
		}
		if newName != oldName {
			newCachePath, _ := gateway.CachePath(home, profileName, newName)
			fmt.Fprintf(out, "✓ Moved cache %s → %s\n",
				homepath.Tilde(oldCachePath, home), homepath.Tilde(newCachePath, home))
		}
		materializeGatewayFromCache(out, updatedCtx, newName, newGW, oldCat, home)
	}

	reconcileProfileHarnesses(updatedCtx, profileName, newName, home)
	return nil
}

func materializeGatewayFromCache(out io.Writer, ctx config.Context, name string, gw config.GatewayConfig, cat *gateway.CachedCatalogue, home string) {
	var statuses []gateway.HarnessFileStatus
	for _, h := range gw.EffectiveHarnesses() {
		if !gateway.CanSyncHarness(ctx, name, h) {
			continue
		}
		st, err := gateway.MaterializeHarness(ctx, name, gw, cat, h, home, false)
		if err != nil {
			st.State, st.Detail = gateway.StateStale, err.Error()
		}
		if st.State == gateway.StateSynced {
			statuses = append(statuses, st)
		}
	}
	if len(statuses) > 0 {
		printHarnessStatuses(out, statuses, home)
	}
}

func reconcileProfileHarnesses(ctx config.Context, profileName, skipGateway, home string) {
	if home == "" {
		return
	}
	for _, g := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		if g == skipGateway {
			continue
		}
		gw := ctx.Gateways[g]
		cat, err := gateway.ReadCache(home, profileName, g)
		if err != nil || !gateway.MatchesBaseURL(cat, gw.BaseURL) {
			continue
		}
		for _, h := range []string{"claude", "codex"} {
			if !slices.Contains(gw.EffectiveHarnesses(), h) {
				continue
			}
			if gateway.CanSyncHarness(ctx, g, h) {
				_, _ = gateway.MaterializeHarness(ctx, g, gw, cat, h, home, false)
			}
		}
	}
}

func updateRepoPrefsGatewayName(home, profileName, oldName, newName string) error {
	if home == "" {
		return nil
	}
	return prefs.Update(prefs.DefaultPath(home), func(f *prefs.File) error {
		remotes := f.Profiles[profileName]
		if len(remotes) == 0 {
			return prefs.ErrUnchanged
		}
		changed := false
		for remote, p := range remotes {
			if p.LegacyCredentials == nil {
				continue
			}
			creds := slices.Clone(*p.LegacyCredentials)
			for i, c := range creds {
				if c == oldName {
					creds[i] = newName
					changed = true
				}
			}
			p.LegacyCredentials = &creds
			remotes[remote] = p
		}
		if !changed {
			return prefs.ErrUnchanged
		}
		return nil
	})
}

func updateRepoPrefsGatewayRemove(home, profileName, name string) error {
	if home == "" {
		return nil
	}
	return prefs.Update(prefs.DefaultPath(home), func(f *prefs.File) error {
		remotes := f.Profiles[profileName]
		if len(remotes) == 0 {
			return prefs.ErrUnchanged
		}
		changed := false
		for remote, p := range remotes {
			if p.LegacyCredentials == nil || !slices.Contains(*p.LegacyCredentials, name) {
				continue
			}
			creds := slices.DeleteFunc(slices.Clone(*p.LegacyCredentials), func(c string) bool { return c == name })
			if len(creds) == 0 {
				p.LegacyCredentials = nil
			} else {
				p.LegacyCredentials = &creds
			}
			f.Set(prefs.Key{Profile: profileName, Remote: remote}, p)
			changed = true
		}
		if !changed {
			return prefs.ErrUnchanged
		}
		return nil
	})
}

// resolveGatewayProfile returns the named profile, or the one matching
// cwd.
func resolveGatewayProfile(cfg *config.Config, cwd, profile string) (string, config.Context, error) {
	if profile != "" {
		ctx, ok := cfg.Contexts[profile]
		if !ok {
			return "", config.Context{}, fmt.Errorf("profile %q not found", profile)
		}
		return profile, ctx, nil
	}
	rc, err := aidectx.Resolve(cfg, cwd, aidectx.GitRemotes(cwd))
	if err != nil {
		return "", config.Context{}, fmt.Errorf("no profile matches this directory (pass --profile): %w", err)
	}
	return rc.Name, cfg.Contexts[rc.Name], nil
}

// stdinIsTTY reports whether stdin is a terminal; tests replace it.
var stdinIsTTY = isStdinTTY

// readGatewayURL returns rawURL or prompts for it on a terminal when
// stdin is not already reserved for --token-stdin.
func readGatewayURL(cmd *cobra.Command, reader *bufio.Reader, rawURL string, tokenStdin bool) (string, error) {
	if rawURL != "" {
		return rawURL, nil
	}
	if tokenStdin || !stdinIsTTY() {
		return "", errors.New("--url is required (the gateway base URL, e.g. https://gateway.example.com)")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Gateway base URL (e.g. https://gateway.example.com): ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the gateway URL: %w", err)
	}
	rawURL = strings.TrimSpace(line)
	if rawURL == "" {
		return "", errors.New("the gateway URL is empty")
	}
	return rawURL, nil
}

// resolveGatewayAddHarnesses returns the harness list for `gateway add`.
// --harnesses wins with no I/O. A TTY (except --token-stdin) prompts with
// the default set in brackets. Otherwise the default is printed and nil
// (all harnesses) is returned.
func resolveGatewayAddHarnesses(cmd *cobra.Command, reader *bufio.Reader, flagValue string, flagSet, tokenStdin bool, presetDefault []string) ([]string, error) {
	if flagSet {
		return parseGatewayHarnesses(flagValue), nil
	}
	shown := presetDefault
	if len(shown) == 0 {
		shown = config.DefaultGatewayHarnesses
	}
	if !tokenStdin && stdinIsTTY() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Harnesses [%s]: ", strings.Join(shown, ","))
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reading the harnesses: %w", err)
		}
		if picked := parseGatewayHarnesses(line); len(picked) > 0 {
			return picked, nil
		}
		return presetDefault, nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Harnesses: %s  (pass --harnesses to change)\n", strings.Join(shown, ", "))
	return presetDefault, nil
}

// readGatewayName prompts for the gateway name on an interactive TTY,
// defaulting to derived when non-empty.
func readGatewayName(cmd *cobra.Command, reader *bufio.Reader, derived string) (string, error) {
	if derived != "" && config.IsGatewayName(derived) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Gateway name [%s]: ", derived)
	} else {
		derived = ""
		fmt.Fprint(cmd.ErrOrStderr(), "Gateway name: ")
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the gateway name: %w", err)
	}
	name := strings.TrimSpace(line)
	if name == "" {
		name = derived
	}
	if name == "" {
		return "", errors.New("the gateway name is empty")
	}
	return name, nil
}

// gatewayNameFromURL is the first DNS label of rawURL's host:
// gateway.example.com names the gateway "gateway".
func gatewayNameFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("--url %q has no host to name the gateway after; pass a name", rawURL)
	}
	label, _, _ := strings.Cut(u.Hostname(), ".")
	return label, nil
}

var errGatewayAddAborted = errors.New("aborted")

func resolveGatewayAddAuth(ctx context.Context, cmd *cobra.Command, reader *bufio.Reader, gw config.GatewayConfig, tokenStdin bool, tokenEnv string, noAuth bool) (token string, models []gateway.Model, err error) {
	if noAuth {
		models, err = gateway.FetchModels(ctx, nil, gateway.Endpoint(gw), "")
		return "", models, err
	}
	if tokenStdin || tokenEnv != "" {
		token, err = readGatewayToken(cmd, reader, tokenStdin, tokenEnv, false)
		if err != nil {
			return "", nil, err
		}
		models, err = gateway.FetchModels(ctx, nil, gateway.Endpoint(gw), token)
		return token, models, err
	}
	models, err = gateway.FetchModels(ctx, nil, gateway.Endpoint(gw), "")
	switch {
	case err == nil && gw.IsLoopback():
		return "", models, nil
	case err == nil && !gw.IsLoopback(), errors.Is(err, gateway.ErrUnauthorized), errors.Is(err, gateway.ErrForbidden):
		if !stdinIsTTY() {
			if err == nil {
				return "", nil, errors.New("this gateway needs a bearer token; pass --key-stdin or --from-env=VAR")
			}
			return "", nil, fmt.Errorf("%w (pass --key-stdin or --from-env=VAR)", err)
		}
		return promptGatewayTokenUntilOK(ctx, cmd, reader, gateway.Endpoint(gw))
	default:
		return "", nil, err
	}
}

func promptGatewayTokenUntilOK(ctx context.Context, cmd *cobra.Command, reader *bufio.Reader, ep gateway.EndpointSpec) (string, []gateway.Model, error) {
	for {
		token, err := readGatewayToken(cmd, reader, false, "", true)
		if err != nil {
			return "", nil, err
		}
		models, err := gateway.FetchModels(ctx, nil, ep, token)
		if err == nil {
			return token, models, nil
		}
		if errors.Is(err, gateway.ErrUnauthorized) || errors.Is(err, gateway.ErrForbidden) {
			fmt.Fprintln(cmd.ErrOrStderr(), "✗ HTTP error — that token was rejected.")
			continue
		}
		return "", nil, err
	}
}

// readGatewayToken reads the token from --token-env, --token-stdin or
// a hidden prompt, in that order. allowEmpty treats a blank TTY/stdin
// token as abort.
func readGatewayToken(cmd *cobra.Command, reader *bufio.Reader, fromStdin bool, envVar string, allowEmpty bool) (string, error) {
	var token string
	switch {
	case envVar != "":
		token = os.Getenv(envVar)
		if token == "" {
			return "", fmt.Errorf("--from-env=%s: the variable is unset or empty", envVar)
		}
	case fromStdin:
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading the token from stdin: %w", err)
		}
		token = strings.TrimRight(line, "\r\n")
	case stdinIsTTY():
		fmt.Fprint(cmd.ErrOrStderr(), "Gateway bearer token (input hidden): ")
		var (
			raw []byte
			err error
		)
		if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) { // #nosec G115 -- stdin fd is always 0/1/2
			raw, err = term.ReadPassword(int(f.Fd())) // #nosec G115
		} else {
			var line string
			line, err = reader.ReadString('\n')
			if errors.Is(err, io.EOF) {
				err = nil
			}
			raw = []byte(line)
		}
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("reading the token: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	default:
		return "", errors.New("no token: stdin is not a terminal; pass --key-stdin or --from-env=VAR")
	}
	if token == "" {
		if allowEmpty {
			return "", errGatewayAddAborted
		}
		return "", errors.New("the gateway token is empty")
	}
	return token, nil
}

// isDefaultFor reports whether name is what harness h uses by default:
// the profile's gateway choice for a gateway, the default credential
// rule for a provider credential.
func isDefaultFor(ctx config.Context, name, h string) bool {
	if _, isGateway := ctx.Gateways[name]; isGateway {
		return ctx.GatewayChoice[h] == name
	}
	// An explicit gateway choice displaces a provider credential default.
	if ctx.GatewayChoice[h] != "" && ctx.Credentials[name].IsLLMProvider(name) {
		return false
	}
	return slices.Contains(config.DefaultCredentialsForHarness(ctx, h), name)
}

// checkKeyUnclaimed refuses a key that already holds a value in store
// unless a gateway owns it (re-adding a gateway rotates its own token).
func checkKeyUnclaimed(cfg *config.Config, store, key string) error {
	for _, ctx := range cfg.Contexts {
		for _, gw := range ctx.Gateways {
			if gw.Store == store && gw.Key == key {
				return nil
			}
		}
	}
	path := config.ResolveSecretPath(store)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	data, err := secrets.LoadSecretsMap(path)
	if err != nil {
		return fmt.Errorf("checking secret store %q for key %q: %w", store, key, err)
	}
	if _, taken := data[key]; taken {
		return fmt.Errorf("secret store %q already has key %q and no gateway owns it; pass another --key, or --force to overwrite it", store, key)
	}
	return nil
}

// secretRefRE matches a template reference to a secret key.
func secretRefRE(key string) *regexp.Regexp {
	k := regexp.QuoteMeta(key)
	return regexp.MustCompile(`\.secrets\.` + k + `\b|index\s+\.secrets\s+"` + k + `"`)
}

// secretUser names what still reads key from store, or "": a gateway or
// credential binding on any profile, or an env: or MCP server template
// on a profile whose secret: is store.
func secretUser(cfg *config.Config, store, key string) string {
	ref := secretRefRE(key)
	for _, p := range slices.Sorted(maps.Keys(cfg.Contexts)) {
		ctx := cfg.Contexts[p]
		for _, g := range slices.Sorted(maps.Keys(ctx.Gateways)) {
			if gw := ctx.Gateways[g]; gw.Store == store && gw.Key == key {
				return fmt.Sprintf("gateway %q on profile %q", g, p)
			}
		}
		for _, c := range slices.Sorted(maps.Keys(ctx.Credentials)) {
			if b := ctx.Credentials[c]; b.Store == store && b.Key == key {
				return fmt.Sprintf("credential %q on profile %q", c, p)
			}
		}
		if ctx.Secret != store {
			continue
		}
		for _, k := range slices.Sorted(maps.Keys(ctx.Env)) {
			if ref.MatchString(ctx.Env[k]) {
				return fmt.Sprintf("env: %s on profile %q", k, p)
			}
		}
		for _, s := range slices.Sorted(maps.Keys(cfg.MCPServers)) {
			for _, v := range cfg.MCPServers[s].Env {
				if ref.MatchString(v) {
					return fmt.Sprintf("MCP server %q (with profile %q)", s, p)
				}
			}
		}
	}
	return ""
}

func definedGateways(ctx config.Context) string {
	if len(ctx.Gateways) == 0 {
		return "none"
	}
	return strings.Join(slices.Sorted(maps.Keys(ctx.Gateways)), ", ")
}

// printHarnessStatuses prints one line per harness after a sync.
func printHarnessStatuses(w io.Writer, statuses []gateway.HarnessFileStatus, home string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, st := range statuses {
		mark := "✓"
		detail := string(st.State)
		switch st.State {
		case gateway.StatePending:
			mark, detail = "-", "pending, dir not created yet; will sync on first launch"
		case gateway.StateStale:
			mark, detail = "!", "stale, "+st.Detail
		default:
			if !gateway.SingleGatewayPerDir(st.Harness) {
				detail += fmt.Sprintf(", %s, env: %s", st.Detail, st.EnvVar)
			} else {
				detail += ", " + st.Detail
			}
		}
		fmt.Fprintf(tw, "  %s %s\t%s\t(%s)\n", mark, st.Harness, homepath.Tilde(st.FilePath, home), detail)
	}
	_ = tw.Flush()
}

func credentialListEntry(profile string, ctx config.Context, name string, b config.CredentialBinding) GatewayListEntry {
	e := GatewayListEntry{
		Profile:    profile,
		Gateway:    name,
		Kind:       "credential",
		BaseURL:    b.EndpointURL(name, ""),
		Store:      b.Store,
		Key:        b.Key,
		DefaultFor: []string{},
	}
	for _, h := range gatewayCredentialHarnesses {
		if !b.DefaultAllowsHarness(name, h) {
			continue
		}
		r, ok := b.Route(name, h)
		if !ok {
			continue
		}
		isDef := isDefaultFor(ctx, name, h)
		if isDef {
			e.DefaultFor = append(e.DefaultFor, h)
		}
		state := gateway.HarnessSyncState("available")
		detail := "select with --gateway " + name
		if isDef {
			state = "active"
			detail = "default"
		}
		e.Harnesses = append(e.Harnesses, gateway.HarnessFileStatus{
			Harness:  h,
			EnvVar:   strings.Join(r.Env, ", "),
			FilePath: "-",
			State:    state,
			Detail:   detail,
		})
	}
	return e
}

func gatewayListEntry(profile string, ctx config.Context, name, home string, withModels bool) GatewayListEntry {
	gw := ctx.Gateways[name]
	ttl := gw.TTL()
	e := GatewayListEntry{
		Profile: profile, Gateway: name, Kind: "gateway", BaseURL: gw.BaseURL, Store: gw.Store, Key: gw.Key,
		RefreshTTL: cmp.Or(gw.RefreshTTL, "24h"), DefaultFor: []string{},
	}
	e.CachePath, _ = gateway.CachePath(home, profile, name)
	cat, _ := gateway.ReadCache(home, profile, name)
	e.ttl = ttl
	switch {
	case cat == nil:
		e.Expired = true
	case !gateway.MatchesBaseURL(cat, gw.BaseURL):
		e.URLMismatch = true
		e.Expired = true
	default:
		e.Expired = gateway.IsExpired(cat, ttl, time.Now())
		e.FetchedAt = &cat.FetchedAt
		e.ModelCount = len(cat.Models)
		if withModels {
			e.Models = cat.Models
		}
	}
	for _, h := range gw.EffectiveHarnesses() {
		if isDefaultFor(ctx, name, h) {
			e.DefaultFor = append(e.DefaultFor, h)
		}
		e.Harnesses = append(e.Harnesses, gateway.InspectHarness(ctx, name, gw, cat, h, home))
	}
	return e
}

func printGatewayList(w io.Writer, entries []GatewayListEntry, cfg *config.Config, home string, withModels bool) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No gateways configured. Add one with: aide gateway add <name> --url <base-url>")
		return
	}
	lastProfile := ""
	for _, e := range entries {
		if e.Profile != lastProfile {
			fmt.Fprintf(w, "PROFILE: %s\n", e.Profile)
			lastProfile = e.Profile
		}
		if len(e.DefaultFor) > 0 {
			defaults := "default for: " + strings.Join(e.DefaultFor, ", ")
			fmt.Fprintf(w, "  ● GATEWAY: %s  [ACTIVE — %s]\n", e.Gateway, defaults)
		} else {
			switchHint := ""
			if e.Kind != "credential" {
				switchHint = fmt.Sprintf("switch: aide gateway use %s --harness <h> --profile %s, or ", e.Gateway, e.Profile)
			}
			fmt.Fprintf(w, "  ○ GATEWAY: %s  [standby — %slaunch: aide --gateway %s]\n", e.Gateway, switchHint, e.Gateway)
		}
		urlDisplay := e.BaseURL
		if e.Kind == "credential" && config.IsBuiltinCredential(e.Gateway) && urlDisplay != "" {
			urlDisplay += " (built-in)"
		}
		if urlDisplay != "" {
			fmt.Fprintf(w, "    URL:          %s\n", urlDisplay)
		}
		if e.Store != "" || e.Key != "" {
			fmt.Fprintf(w, "    Secret:       store=%s, key=%s\n", e.Store, e.Key)
		}
		if len(e.ChosenFor) > 0 {
			parts := make([]string, len(e.ChosenFor))
			for i, c := range e.ChosenFor {
				parts[i] = fmt.Sprintf("%s (%s)", c.Harness, c.Source)
			}
			fmt.Fprintf(w, "    Chosen for:   %s\n", strings.Join(parts, ", "))
		}
		if e.Kind != "credential" {
			fmt.Fprintf(w, "    Refresh TTL:  %s\n", e.RefreshTTL)
			fmt.Fprintf(w, "    Cache:        %s\n", cacheSummary(e))
			fmt.Fprintf(w, "                  %s\n", homepath.Tilde(e.CachePath, home))
		}
		fmt.Fprintln(w, "    Harnesses:")
		gw := cfg.Contexts[e.Profile].Gateways[e.Gateway]
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "      HARNESS\tENV VAR\tTARGET FILE\tSTATUS")
		for _, st := range e.Harnesses {
			status := string(st.State)
			if st.Detail != "" {
				status += " (" + st.Detail + ")"
			}
			envVar := st.EnvVar
			if envVar == "" && e.Kind != "credential" {
				envVar = gw.TokenEnv(e.Gateway, st.Harness)
			}
			targetFile := st.FilePath
			if targetFile != "-" {
				targetFile = homepath.Tilde(targetFile, home)
			}
			fmt.Fprintf(tw, "      %s\t%s\t%s\t%s\n", st.Harness, envVar, targetFile, status)
		}
		_ = tw.Flush()
		if withModels && e.Kind != "credential" {
			fmt.Fprintf(w, "    Models (%d):\n", len(e.Models))
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "      ID\tDISPLAY NAME\tPROVIDER\tCONTEXT\tMAX OUTPUT")
			for _, m := range e.Models {
				// The cache file is local but its contents came from the
				// gateway: strip control characters again before printing.
				fmt.Fprintf(tw, "      %s\t%s\t%s\t%d\t%d\n", gateway.Sanitize(m.ID), gateway.Sanitize(m.DisplayName),
					gateway.Sanitize(m.Provider), m.ContextWindow, m.MaxOutputTokens)
			}
			_ = tw.Flush()
		}
	}
}

func cacheSummary(e GatewayListEntry) string {
	if e.URLMismatch {
		return "base URL changed (run: aide gateway sync " + e.Gateway + ")"
	}
	if e.FetchedAt == nil {
		return "no cached models yet (run: aide gateway sync " + e.Gateway + ")"
	}
	age := time.Since(*e.FetchedAt)
	if e.Expired {
		return fmt.Sprintf("%d models (synced %s ago; expired, refreshes on next launch)", e.ModelCount, gateway.FormatAge(age))
	}
	return fmt.Sprintf("%d models (synced %s ago; fresh, next auto-refresh in %s)", e.ModelCount, gateway.FormatAge(age), gateway.FormatAge(e.ttl-age))
}

// userHomeDir is the user's home directory, or "" when unknown (paths
// are then printed in full).
func userHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
