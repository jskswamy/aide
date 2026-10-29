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
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/secrets"
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

	ttl time.Duration
}

func gatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Manage custom LLM gateways and sync their models into harness configs",
		Long: `Manage custom LLM gateways and synchronize model catalogues across harnesses.

A gateway is an Anthropic- and OpenAI-compatible endpoint (for example
LiteLLM) that aide injects into claude, pi, opencode and codex. Its
bearer token lives in an encrypted secret store; harness config files
only name the variable aide injects it as.`,
	}
	cmd.AddCommand(gatewayAddCmd())
	cmd.AddCommand(gatewayListCmd())
	cmd.AddCommand(gatewayUseCmd())
	cmd.AddCommand(gatewayEditCmd())
	cmd.AddCommand(gatewayRenameCmd())
	cmd.AddCommand(gatewaySyncCmd())
	cmd.AddCommand(gatewayRemoveCmd())
	return cmd
}

func gatewayAddCmd() *cobra.Command {
	var (
		rawURL, profile, ttl, harnesses, store, key, ageKey, tokenEnv string
		tokenStdin, force, makeDefault                                bool
	)
	cmd := &cobra.Command{
		Use:   "add [<name>]",
		Short: "Register a custom gateway, encrypt its token, and sync models",
		Long: `Register a custom gateway on a profile, encrypt its bearer token into
the profile's secret store, and sync its models into harness configs.

The base URL is read from --url or prompted on a terminal. The token is
read from a hidden prompt, from stdin (--token-stdin) or from a named
environment variable (--token-env); never from a flag value, which would
land in shell history.

Examples:
  aide gateway add corp --url https://gateway.example.com --profile work
  op read op://Eng/Gateway/credential | aide gateway add corp --url https://gateway.example.com --token-stdin
  aide gateway add corp --url https://gateway.example.com --token-env CORP_GATEWAY_TOKEN --harnesses claude,pi`,
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
			cfg := env.Config()
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), profile)
			if err != nil {
				return err
			}
			rawURL, err = readGatewayURL(cmd, reader, rawURL, tokenStdin)
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				derived, deriveErr := gatewayNameFromURL(rawURL)
				if !tokenStdin && stdinIsTTY() {
					name, err = readGatewayName(cmd, reader, derived)
					if err != nil {
						return err
					}
				} else if deriveErr != nil {
					return deriveErr
				} else {
					name = derived
				}
			}

			gw := config.GatewayConfig{
				BaseURL:    gateway.NormalizeBaseURL(rawURL),
				RefreshTTL: ttl,
				Store:      store,
				Key:        key,
				Harnesses:  parseGatewayHarnesses(harnesses),
			}
			if gw.Store == "" {
				gw.Store = ctx.Secret
			}
			if gw.Store == "" {
				gw.Store = profileName
			}
			if gw.Key == "" {
				gw.Key = defaultGatewayKey(name)
			}
			if old, ok := ctx.Gateways[name]; ok && !force && gateway.NormalizeBaseURL(old.BaseURL) != gw.BaseURL {
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

			if !force {
				if err := checkKeyUnclaimed(cfg, gw.Store, gw.Key); err != nil {
					return err
				}
			}

			token, err := readGatewayToken(cmd, reader, tokenStdin, tokenEnv)
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("default") {
				if makeDefault {
					ctx.DefaultCredentials = setProfileDefaultCredential(ctx, name)
				}
			} else if existing := collidingDefaultForGateway(ctx, name, gw); existing != "" && !tokenStdin && stdinIsTTY() {
				if promptMakeDefault(cmd, reader, profileName, existing, name) {
					ctx.DefaultCredentials = setProfileDefaultCredential(ctx, name)
				}
			}

			secretsDir := config.SecretsDir()
			if err := secrets.NewManager(secretsDir, "").SetKey(gw.Store, secretsDir, gw.Key, token, ageKey); err != nil {
				return fmt.Errorf("storing the gateway token: %w", err)
			}
			home := userHomeDir()
			fmt.Fprintf(out, "✓ Encrypted token into %s (key: %s)\n",
				homepath.Tilde(config.ResolveSecretPath(gw.Store), home), gw.Key)

			cfg.Contexts[profileName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "✓ Saved gateway %q under profile %q in %s\n", name, profileName, homepath.Tilde(config.FilePath(), home))

			res, err := gateway.Sync(context.Background(), gateway.SyncOptions{
				ProfileName: profileName, Profile: ctx, GatewayName: name, Gateway: gw,
				Token: token, HomeDir: home, Progress: out,
			})
			if err != nil {
				return fmt.Errorf("gateway %q is saved, but the first model sync failed (retry with: aide gateway sync %s --profile %s): %w",
					name, name, profileName, err)
			}
			printHarnessStatuses(out, res.HarnessStatuses, gw, home)
			return nil
		},
	}
	cmd.Flags().StringVar(&rawURL, "url", "", "Gateway base URL (https://, or http:// for localhost only)")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read the bearer token from stdin")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "Read the bearer token from this environment variable")
	cmd.Flags().StringVar(&ttl, "ttl", "24h", "Launch-time model refresh interval (e.g. 12h, 24h, 168h)")
	cmd.Flags().StringVar(&harnesses, "harnesses", "", "Comma-separated harnesses to configure (default: claude,pi,opencode,codex)")
	cmd.Flags().StringVar(&store, "store", "", "Secret store for the token (default: the profile's secret store, else the profile name)")
	cmd.Flags().StringVar(&key, "key", "", "Key for the token inside the store (default: <name>_token)")
	cmd.Flags().StringVar(&ageKey, "age-key", "", "age1... recipient for a new secret store (default: the discovered age identity)")
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "Make this gateway the profile default across its harnesses")
	cmd.Flags().BoolVar(&force, "force", false, "Replace a gateway of the same name with another URL, or overwrite an existing secret key")
	return cmd
}

func gatewayListCmd() *cobra.Command {
	var (
		profile string
		models  bool
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
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg := env.Config()
			if profile != "" {
				if _, ok := cfg.Contexts[profile]; !ok {
					return fmt.Errorf("profile %q not found", profile)
				}
			}
			home := userHomeDir()
			entries := []GatewayListEntry{}
			for _, p := range slices.Sorted(maps.Keys(cfg.Contexts)) {
				if profile != "" && p != profile {
					continue
				}
				ctx := cfg.Contexts[p]
				for _, name := range slices.Sorted(maps.Keys(ctx.Credentials)) {
					b := ctx.Credentials[name]
					if !b.IsLLMProvider(name) {
						continue
					}
					entries = append(entries, credentialListEntry(p, ctx, name, b))
				}
				for _, name := range slices.Sorted(maps.Keys(ctx.Gateways)) {
					entries = append(entries, gatewayListEntry(p, ctx, name, home, models))
				}
			}
			return output.Emit(cmd.OutOrStdout(), format, entries, func(w io.Writer) error {
				printGatewayList(w, entries, cfg, home, models)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Show only this profile's gateways")
	cmd.Flags().BoolVar(&models, "models", false, "Include each gateway's cached models")
	return cmd
}

type selectableGateway struct {
	name       string
	kind       string
	url        string
	defaultFor []string
}

func selectableGateways(ctx config.Context) []selectableGateway {
	var list []selectableGateway
	for _, name := range slices.Sorted(maps.Keys(ctx.Credentials)) {
		b := ctx.Credentials[name]
		if !b.IsLLMProvider(name) {
			continue
		}
		u := b.EndpointURL(name, "")
		if config.IsBuiltinCredential(name) && u != "" {
			u += " (built-in)"
		}
		var defs []string
		for _, h := range []string{"claude", "pi", "opencode", "codex", "gemini"} {
			if gateway.IsDefaultGateway(ctx, name, h) {
				defs = append(defs, h)
			}
		}
		list = append(list, selectableGateway{name: name, kind: "credential", url: u, defaultFor: defs})
	}
	for _, name := range slices.Sorted(maps.Keys(ctx.Gateways)) {
		gw := ctx.Gateways[name]
		var defs []string
		for _, h := range gw.EffectiveHarnesses() {
			if gateway.IsDefaultGateway(ctx, name, h) {
				defs = append(defs, h)
			}
		}
		list = append(list, selectableGateway{name: name, kind: "gateway", url: gw.BaseURL, defaultFor: defs})
	}
	return list
}

func gatewayUseCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "use [<name>]",
		Short: "Set the profile's default gateway or built-in credential",
		Long: `Switch a profile's default gateway or built-in provider credential.

Without <name> on a terminal, lists the profile's configured gateways and
built-in provider credentials and prompts for a selection.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(cmd.InOrStdin())
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg := env.Config()
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), profile)
			if err != nil {
				return err
			}
			choices := selectableGateways(ctx)
			if len(choices) == 0 {
				return fmt.Errorf("profile %q has no gateways or provider credentials configured; add one with: aide gateway add", profileName)
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				if !stdinIsTTY() {
					return errors.New("gateway or credential name is required when stdin is not a terminal")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Gateways on profile %q:\n", profileName)
				tw := tabwriter.NewWriter(cmd.ErrOrStderr(), 0, 0, 2, ' ', 0)
				for i, c := range choices {
					status := "[standby]"
					if len(c.defaultFor) > 0 {
						status = "[default: " + strings.Join(c.defaultFor, ", ") + "]"
					}
					fmt.Fprintf(tw, "  [%d] %s\t%s\t%s\n", i+1, c.name, c.url, status)
				}
				_ = tw.Flush()
				fmt.Fprint(cmd.ErrOrStderr(), "Select default [1]: ")
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("reading selection: %w", err)
				}
				ans := strings.TrimSpace(line)
				if ans == "" {
					name = choices[0].name
				} else if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(choices) {
					name = choices[idx-1].name
				} else {
					name = ans
				}
			}

			found := false
			for _, c := range choices {
				if c.name == name {
					found = true
					break
				}
			}
			if !found {
				var names []string
				for _, c := range choices {
					names = append(names, c.name)
				}
				return fmt.Errorf("profile %q has no gateway or provider credential %q (defined: %s)",
					profileName, name, strings.Join(names, ", "))
			}

			ctx.DefaultCredentials = setProfileDefaultCredential(ctx, name)
			cfg.Contexts[profileName] = ctx
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}

			var activeOn []string
			for _, h := range []string{"claude", "pi", "opencode", "codex", "gemini"} {
				if gateway.IsDefaultGateway(ctx, name, h) {
					activeOn = append(activeOn, h)
				}
			}
			fmt.Fprintf(out, "✓ Set default gateway for profile %q to %q (%s)\n",
				profileName, name, strings.Join(activeOn, ", "))

			home := userHomeDir()
			if gw, isGw := ctx.Gateways[name]; isGw {
				if cat, err := gateway.ReadCache(home, profileName, name); err == nil && cat != nil {
					materializeGatewayFromCache(out, ctx, name, gw, cat, home)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	return cmd
}

func gatewayEditCmd() *cobra.Command {
	var (
		flagName, rawURL, profile, ttl, harnesses, store, key, ageKey, tokenEnv string
		rotateToken, tokenStdin, force                                          bool
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
			cfg := env.Config()
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), profile)
			if err != nil {
				return err
			}
			oldName, err := resolveEditableGatewayName(cmd, reader, profileName, ctx, args, tokenStdin)
			if err != nil {
				return err
			}
			oldGW := ctx.Gateways[oldName]

			hasEditFlags := cmd.Flags().Changed("name") ||
				cmd.Flags().Changed("url") ||
				cmd.Flags().Changed("ttl") ||
				cmd.Flags().Changed("harnesses") ||
				cmd.Flags().Changed("store") ||
				cmd.Flags().Changed("key") ||
				rotateToken || tokenStdin || tokenEnv != ""

			newName := oldName
			newGW := oldGW
			wantTokenRotate := rotateToken || tokenStdin || tokenEnv != ""

			if hasEditFlags {
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
			} else {
				if !stdinIsTTY() {
					return errors.New("no changes specified; pass at least one flag (--name, --url, --ttl, --harnesses, --store, --key, --token-stdin, --token-env) or run on a terminal")
				}
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
				wantTokenRotate = promptRotateToken(cmd, reader)
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
				newToken, err = readGatewayToken(cmd, reader, tokenStdin, tokenEnv)
				if err != nil {
					return err
				}
			}

			return applyGatewayEdit(cmd.Context(), out, cmd.ErrOrStderr(), cfg, profileName, oldName, newName, oldGW, newGW, newToken, ageKey, wantTokenRotate, force)
		},
	}
	cmd.Flags().StringVar(&flagName, "name", "", "Rename the gateway to this name")
	cmd.Flags().StringVar(&rawURL, "url", "", "Updated gateway base URL (https://, or http:// for localhost only)")
	cmd.Flags().StringVar(&ttl, "ttl", "", "Updated launch-time model refresh interval (e.g. 12h, 24h, 168h)")
	cmd.Flags().StringVar(&harnesses, "harnesses", "", "Comma-separated harnesses to configure, or 'all' (default: claude,pi,opencode,codex)")
	cmd.Flags().BoolVar(&rotateToken, "rotate-token", false, "Prompt for a new bearer token and re-sync models")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read a new bearer token from stdin")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "Read a new bearer token from this environment variable")
	cmd.Flags().StringVar(&store, "store", "", "Move the token to this secret store")
	cmd.Flags().StringVar(&key, "key", "", "Secret key name inside the store")
	cmd.Flags().StringVar(&ageKey, "age-key", "", "age1... recipient for a new secret store")
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing unclaimed secret key in the target store")
	return cmd
}

func gatewayRenameCmd() *cobra.Command {
	var (
		profile, ageKey string
		force           bool
	)
	cmd := &cobra.Command{
		Use:   "rename <old-name> [<new-name>]",
		Short: "Rename a gateway and migrate its secret key, cache, and harness configs",
		Long: `Rename an existing custom gateway on a profile, automatically migrating its
encrypted secret key (<old>_token -> <new>_token), default_credentials entry,
repository preferences, cached model catalogue, and harness provider entries.`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			reader := bufio.NewReader(cmd.InOrStdin())
			env, err := cmdEnv(cmd)
			if err != nil {
				return err
			}
			cfg := env.Config()
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), profile)
			if err != nil {
				return err
			}
			oldName := args[0]
			oldGW, ok := ctx.Gateways[oldName]
			if !ok {
				return fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, oldName, definedGateways(ctx))
			}
			newName := ""
			if len(args) == 2 {
				newName = strings.TrimSpace(args[1])
			} else {
				if !stdinIsTTY() {
					return errors.New("new gateway name is required when stdin is not a terminal")
				}
				fmt.Fprint(cmd.ErrOrStderr(), "New gateway name: ")
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("reading the new gateway name: %w", err)
				}
				newName = strings.TrimSpace(line)
			}
			if newName == "" {
				return errors.New("the new gateway name is empty")
			}
			if newName == oldName {
				return fmt.Errorf("gateway %q already has that name", oldName)
			}
			newGW := oldGW
			if oldGW.Key == defaultGatewayKey(oldName) {
				newGW.Key = defaultGatewayKey(newName)
			}
			return applyGatewayEdit(cmd.Context(), out, cmd.ErrOrStderr(), cfg, profileName, oldName, newName, oldGW, newGW, "", ageKey, false, force)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	cmd.Flags().StringVar(&ageKey, "age-key", "", "age1... recipient if migrating to a new secret store")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing unclaimed secret key in the secret store")
	return cmd
}

func gatewaySyncCmd() *cobra.Command {
	var profile string
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
			profileName, ctx, err := resolveGatewayProfile(env.Config(), env.CWD(), profile)
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
				fmt.Fprintf(out, "→ Syncing gateway %q (profile %q)\n", name, profileName)
				token, err := launcher.ResolveStoreKey(load, fmt.Sprintf("gateway %q", name), gw.Store, gw.Key)
				if err == nil {
					var res *gateway.SyncResult
					res, err = gateway.Sync(context.Background(), gateway.SyncOptions{
						ProfileName: profileName, Profile: ctx, GatewayName: name, Gateway: gw,
						Token: token, HomeDir: home, Progress: out,
					})
					if err == nil {
						printHarnessStatuses(out, res.HarnessStatuses, gw, home)
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
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
	return cmd
}

func gatewayRemoveCmd() *cobra.Command {
	var (
		profile    string
		keepSecret bool
	)
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
			cfg := env.Config()
			profileName, ctx, err := resolveGatewayProfile(cfg, env.CWD(), profile)
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
			updated.DefaultCredentials = slices.DeleteFunc(slices.Clone(ctx.DefaultCredentials), func(s string) bool { return s == name })
			cfg.Contexts[profileName] = updated
			if err := config.WriteConfig(cfg); err != nil {
				return fmt.Errorf("writing config: %w", err)
			}
			fmt.Fprintf(out, "✓ Removed gateway %q from profile %q in %s\n", name, profileName, homepath.Tilde(config.FilePath(), home))

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
	cmd.Flags().StringVar(&profile, "profile", "", "Target profile (default: matched from the current directory)")
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

func resolveEditableGatewayName(cmd *cobra.Command, reader *bufio.Reader, profileName string, ctx config.Context, args []string, tokenStdin bool) (string, error) {
	if len(args) == 1 {
		name := args[0]
		if _, ok := ctx.Gateways[name]; !ok {
			return "", fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, name, definedGateways(ctx))
		}
		return name, nil
	}
	names := slices.Sorted(maps.Keys(ctx.Gateways))
	if len(names) == 0 {
		return "", fmt.Errorf("profile %q has no gateways; add one with: aide gateway add", profileName)
	}
	if tokenStdin || !stdinIsTTY() {
		return "", errors.New("gateway name is required when stdin is not a terminal")
	}
	if len(names) == 1 {
		return names[0], nil
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
		return "", fmt.Errorf("reading selection: %w", err)
	}
	ans := strings.TrimSpace(line)
	name := names[0]
	if idx, convErr := strconv.Atoi(ans); convErr == nil && idx >= 1 && idx <= len(names) {
		name = names[idx-1]
	} else if ans != "" {
		name = ans
	}
	if _, ok := ctx.Gateways[name]; !ok {
		return "", fmt.Errorf("profile %q has no gateway %q (defined: %s)", profileName, name, definedGateways(ctx))
	}
	return name, nil
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
	if newName != oldName && len(ctx.DefaultCredentials) > 0 {
		defs := slices.Clone(ctx.DefaultCredentials)
		for i, c := range defs {
			if c == oldName {
				defs[i] = newName
			}
		}
		updatedCtx.DefaultCredentials = defs
	}
	if err := config.ValidateGateways("profiles."+profileName, updatedCtx, cfg.NeverAllowEnv); err != nil {
		return err
	}

	keyOrStoreChanged := newGW.Store != oldGW.Store || newGW.Key != oldGW.Key
	urlChanged := gateway.NormalizeBaseURL(newGW.BaseURL) != gateway.NormalizeBaseURL(oldGW.BaseURL)
	needsLiveSync := tokenChanged || urlChanged

	if keyOrStoreChanged && !force {
		if err := checkKeyUnclaimed(cfg, newGW.Store, newGW.Key); err != nil {
			return err
		}
	}

	activeToken := newToken
	if !tokenChanged && (keyOrStoreChanged || needsLiveSync) {
		load := launcher.CachingStoreLoader(secrets.LoadSecretsMap)
		var err error
		activeToken, err = launcher.ResolveStoreKey(load, fmt.Sprintf("gateway %q", oldName), oldGW.Store, oldGW.Key)
		if err != nil {
			return err
		}
	}

	home := userHomeDir()
	secretsDir := config.SecretsDir()
	if tokenChanged || keyOrStoreChanged {
		if err := secrets.NewManager(secretsDir, "").SetKey(newGW.Store, secretsDir, newGW.Key, activeToken, ageKey); err != nil {
			return fmt.Errorf("storing the gateway token: %w", err)
		}
		if keyOrStoreChanged && !tokenChanged {
			fmt.Fprintf(out, "✓ Migrated secret key %q → %q in %s\n",
				oldGW.Key, newGW.Key, homepath.Tilde(config.ResolveSecretPath(newGW.Store), home))
		} else {
			fmt.Fprintf(out, "✓ Encrypted token into %s (key: %s)\n",
				homepath.Tilde(config.ResolveSecretPath(newGW.Store), home), newGW.Key)
		}
	}

	cfg.Contexts[profileName] = updatedCtx
	if err := config.WriteConfig(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
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

	if needsLiveSync {
		if syncCtx == nil {
			syncCtx = context.Background()
		}
		res, err := gateway.Sync(syncCtx, gateway.SyncOptions{
			ProfileName: profileName, Profile: updatedCtx, GatewayName: newName, Gateway: newGW,
			Token: activeToken, HomeDir: home, Progress: out,
		})
		if err != nil {
			if oldCat != nil && !urlChanged {
				oldCat.Gateway = newName
				oldCat.BaseURL = newGW.BaseURL
				_ = gateway.WriteCache(home, oldCat)
				materializeGatewayFromCache(out, updatedCtx, newName, newGW, oldCat, home)
			}
			reconcileProfileHarnesses(updatedCtx, profileName, newName, home)
			return fmt.Errorf("gateway %q is saved, but model sync failed (retry with: aide gateway sync %s --profile %s): %w",
				newName, newName, profileName, err)
		}
		printHarnessStatuses(out, res.HarnessStatuses, newGW, home)
	} else if oldCat != nil {
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
		printHarnessStatuses(out, statuses, gw, home)
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
		if err != nil || cat == nil {
			continue
		}
		for _, h := range []string{"claude", "codex"} {
			if !slices.Contains(gw.EffectiveHarnesses(), h) {
				continue
			}
			if (h == "claude" && gateway.CanSyncHarness(ctx, g, "claude")) ||
				(h == "codex" && gateway.IsDefaultGateway(ctx, g, "codex")) {
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
			if p.Credentials == nil {
				continue
			}
			creds := slices.Clone(*p.Credentials)
			for i, c := range creds {
				if c == oldName {
					creds[i] = newName
					changed = true
				}
			}
			p.Credentials = &creds
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
			if p.Credentials == nil || !slices.Contains(*p.Credentials, name) {
				continue
			}
			creds := slices.DeleteFunc(slices.Clone(*p.Credentials), func(c string) bool { return c == name })
			if len(creds) == 0 {
				p.Credentials = nil
			} else {
				p.Credentials = &creds
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

// readGatewayToken reads the token from --token-env, --token-stdin or
// a hidden prompt, in that order.
func readGatewayToken(cmd *cobra.Command, reader *bufio.Reader, fromStdin bool, envVar string) (string, error) {
	var token string
	switch {
	case envVar != "":
		token = os.Getenv(envVar)
		if token == "" {
			return "", fmt.Errorf("--token-env %s: the variable is unset or empty", envVar)
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
		return "", errors.New("no token: stdin is not a terminal; pass --token-stdin or --token-env <VAR>")
	}
	if token == "" {
		return "", errors.New("the gateway token is empty")
	}
	return token, nil
}

// collidingDefaultForGateway returns the existing default credential or
// gateway name that claims any of gw's harnesses ahead of name, or ""
// when name is already default across all of gw.EffectiveHarnesses().
func collidingDefaultForGateway(ctx config.Context, name string, gw config.GatewayConfig) string {
	synth, err := config.SynthesizeGatewayCredentials(ctx)
	if err != nil {
		return ""
	}
	for _, h := range gw.EffectiveHarnesses() {
		defs := config.DefaultCredentialsForHarness(synth, h)
		if slices.Contains(defs, name) {
			continue
		}
		for _, d := range defs {
			b := synth.Credentials[d]
			_, isGw := synth.Gateways[d]
			if isGw || b.IsLLMProvider(d) {
				return d
			}
		}
	}
	return ""
}

func promptMakeDefault(cmd *cobra.Command, reader *bufio.Reader, profileName, existing, name string) bool {
	fmt.Fprintf(cmd.ErrOrStderr(), "Profile %q currently uses %q by default. Make %q the default instead? [Y/n]: ",
		profileName, existing, name)
	line, err := reader.ReadString('\n')
	if err != nil && errors.Is(err, io.EOF) && line == "" {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "" || ans == "y" || ans == "yes"
}

// setProfileDefaultCredential places name first in ctx.DefaultCredentials
// and removes any other LLM provider or gateway whose harness scope
// overlaps with name, while preserving unrelated non-provider bindings.
func setProfileDefaultCredential(ctx config.Context, name string) []string {
	synth, err := config.SynthesizeGatewayCredentials(ctx)
	if err != nil {
		return []string{name}
	}
	nameB := synth.Credentials[name]
	_, nameIsGw := synth.Gateways[name]
	nameIsProvider := nameIsGw || nameB.IsLLMProvider(name)

	out := []string{name}
	for _, c := range ctx.DefaultCredentials {
		if c == name {
			continue
		}
		cb, ok := synth.Credentials[c]
		if !ok {
			continue
		}
		_, cIsGw := synth.Gateways[c]
		cIsProvider := cIsGw || cb.IsLLMProvider(c)
		if nameIsProvider && cIsProvider && bindingsOverlapHarnesses(name, nameB, c, cb) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func bindingsOverlapHarnesses(nameA string, a config.CredentialBinding, nameB string, b config.CredentialBinding) bool {
	for _, h := range []string{"claude", "pi", "opencode", "codex", "gemini"} {
		if a.DefaultAllowsHarness(nameA, h) && b.DefaultAllowsHarness(nameB, h) {
			return true
		}
	}
	return false
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
func printHarnessStatuses(w io.Writer, statuses []gateway.HarnessFileStatus, gw config.GatewayConfig, home string) {
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
				detail += fmt.Sprintf(", %s, env: %s", st.Detail, gw.TokenEnv(st.Harness))
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
	for _, h := range []string{"claude", "pi", "opencode", "codex", "gemini"} {
		if !b.DefaultAllowsHarness(name, h) {
			continue
		}
		r, ok := b.Route(name, h)
		if !ok {
			continue
		}
		isDef := gateway.IsDefaultGateway(ctx, name, h)
		if isDef {
			e.DefaultFor = append(e.DefaultFor, h)
		}
		state := gateway.HarnessSyncState("standby")
		detail := "select with --credential " + name
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
	e.Expired = gateway.IsExpired(cat, ttl, time.Now())
	e.ttl = ttl
	if cat != nil {
		e.FetchedAt = &cat.FetchedAt
		e.ModelCount = len(cat.Models)
		if withModels {
			e.Models = cat.Models
		}
	}
	for _, h := range gw.EffectiveHarnesses() {
		if gateway.IsDefaultGateway(ctx, name, h) {
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
		defaults := fmt.Sprintf("not a default; switch with: aide gateway use %s --profile %s, or pass --credential %s", e.Gateway, e.Profile, e.Gateway)
		if len(e.DefaultFor) > 0 {
			defaults = "default for: " + strings.Join(e.DefaultFor, ", ")
			if len(e.DefaultFor) < len(e.Harnesses) {
				defaults += fmt.Sprintf("; switch all with: aide gateway use %s --profile %s", e.Gateway, e.Profile)
			}
		}
		fmt.Fprintf(w, "  GATEWAY: %s (%s)\n", e.Gateway, defaults)
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
				envVar = gw.TokenEnv(st.Harness)
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
