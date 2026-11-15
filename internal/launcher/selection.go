package launcher

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gatewaymigrate"
	"github.com/jskswamy/aide/internal/gatewaypreset"
	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/ui"
	"github.com/jskswamy/aide/internal/usage"
)

// Harness and credential selection sources, reported as
// harness_source and credential_source. Profile sources
// (profile_source) are aidectx's Source* constants.
const (
	SourceFlag       = aidectx.SourceFlag // --agent, --credential
	SourceAuthLogin  = "auth"             // --auth login: nothing injected
	SourcePreference = "preference"       // saved with aide project set
	SourceRepository = "repository"       // trusted .aide.yaml agent:
	SourceProfile    = "profile"          // the profile's agent: / default_credentials:
)

// Gateway choice sources, reported as Choice.Source. A flag choice is
// SourceFlag and a profile one SourceProfile.
const (
	SourceRepo         = "repo"          // saved with aide project set
	SourcePicker       = "picker"        // picked at this launch
	SourceNoCandidates = "no-candidates" // no gateway is marked for the harness
	SourceUnset        = "unset"         // marked gateways exist, none chosen
)

// exitConfig is EX_CONFIG: the launch cannot proceed without a choice
// the user has not made.
const exitConfig = 78

// ErrNoTerminal is what a ChoicePrompter returns when there is nobody
// to ask.
var ErrNoTerminal = errors.New("no terminal to ask on")

// Choice is the gateway a harness launches with. Value is "" (unset),
// "none" (the harness's own login) or a gateway name.
type Choice struct {
	Value, Source string
	// Saved is where a picked choice was remembered: "repo", "profile"
	// or "" for this launch only.
	Saved ui.Remember
	// Derived is a choice the legacy default rule made for a profile
	// that never recorded one.
	Derived bool
}

// Segment returns the gateway segment string for prompt and statusline
// ("login" when Value is "none", Value when a gateway is chosen, "?"
// when Source is SourceUnset, or "" when the harness has no gateway reach).
func (c Choice) Segment() string {
	switch {
	case c.Value == gatewayNone:
		return "login"
	case c.Value != "":
		return c.Value
	case c.Source == SourceUnset:
		return "?"
	default:
		return ""
	}
}

// ResolvedChoice is a harness's Choice and what resolving it reported.
type ResolvedChoice struct {
	Choice
	Notices []string
}

// ChoicePrompter asks which of options harness should use in profile,
// and reports how long the answer was remembered.
type ChoicePrompter func(harness, profile string, options []ui.PickerOption) (string, ui.Remember, error)

// SelectionInputs are the inputs to harness and credential selection
// besides the profile itself: one-time launch flags and the
// repository's saved preference.
type SelectionInputs struct {
	Agent string // --agent
	// Credentials is the --credential selection; CredentialsSet
	// reports the flag was given, so an empty selection is explicit.
	Credentials    []string
	CredentialsSet bool
	Auth           AuthMode
	Saved          prefs.Preference
}

// Selection records what a launch selected and where each choice came
// from. Credentials holds binding names only.
type Selection struct {
	ProfileSource    string
	HarnessSource    string
	Credentials      []string
	CredentialSource string
	// requested is the selection before --auth login displaced it.
	requested []string
	// Gateway is the harness's gateway choice, set by
	// PlanProfileCredentials; ChoiceNotices are what resolving it
	// reported.
	Gateway       Choice
	ChoiceNotices []string
	// Choices is every gateway-capable harness's choice, resolved
	// without asking (the launched harness's is Gateway), for display.
	Choices map[string]ResolvedChoice
	// prompt, when set, lets an unset slot choice ask (a launch); nil
	// resolves in passive preview mode.
	prompt ChoicePrompter
	// gatewayFor is the harness Gateway was resolved for.
	gatewayFor string
}

// Resolution is what a launch from a directory selects before it
// decrypts anything.
type Resolution struct {
	// Config is the config selected from; an empty-state action may
	// have reloaded it.
	Config *config.Config
	// Gate is the .aide.yaml trust gate's outcome.
	Gate ProjectGate
	// RC is the selected profile with the final harness and its
	// profile env; nil when no profile was resolved.
	RC *aidectx.ResolvedContext
	// Inputs are the selection inputs with the saved repository
	// preference filled in.
	Inputs SelectionInputs
	Sel    Selection
	// Notices are the notes a launch prints (Gate.Warnings aside).
	Notices []string
	// Refused is why a launch would refuse the selection: its
	// harness's global login is another profile's too.
	Refused error
}

// PassiveChoice resolves the active harness's gateway choice passively
// (without prompting or failing on unset slots) from r's resolved
// profile, config, and repository preference.
func (r Resolution) PassiveChoice() Choice {
	if r.Config == nil || r.RC == nil {
		return Choice{}
	}
	c, _, _ := ResolveChoice(r.Config, r.RC.Name, r.RC.Context.Residual(), r.Config.HarnessOf(r.RC.Context.Agent), r.Inputs, nil)
	return c
}

// ResolveSelection makes a launch's selection from cwd: the .aide.yaml
// trust gate (it mutates cfg), the profile (l.Profile, else matched,
// else default, else l.EmptyStateActions), the saved repository
// preference (not read when homeDir is empty), the final harness and
// its profile env. Launch and the previews (which, status, prompt,
// statusline, sandbox) all call it, so they agree. On error the
// Resolution holds what was resolved so far.
func (l *Launcher) ResolveSelection(cfg *config.Config, cwd, homeDir string, in SelectionInputs) (Resolution, error) {
	res := Resolution{Config: cfg, Inputs: in}
	var err error
	if l.IgnoreProjectConfig {
		cfg.DropProjectConfig()
	} else {
		res.Gate, err = l.applyTrustGate(cfg)
		l.promptProjectTrust = false
		if err != nil {
			return res, err
		}
	}
	remotes := aidectx.GitRemotes(cwd)
	if res.RC, err = aidectx.ResolveSelected(cfg, cwd, remotes, l.Profile); err != nil {
		return res, fmt.Errorf("resolving profile: %w", err)
	}
	rc := res.RC
	if n := ProjectProfileNotice(cfg, rc); n != "" {
		res.Notices = append(res.Notices, n)
	}
	if homeDir != "" {
		if res.Inputs.Saved, err = SavedPreference(l.prefsPath(homeDir), rc.Name, remotes); err != nil {
			return res, err
		}
	}
	// The final harness precedes profile-env injection so the profile
	// directory is the final harness's, not the profile's original one.
	sel, notice, err := SelectHarness(cfg, rc, res.Inputs, l.lookPath())
	res.Sel = sel
	if err != nil {
		return res, err
	}
	if notice != "" {
		res.Notices = append(res.Notices, notice)
	}
	// A profile mis-config must stop the launch rather than silently
	// run against the wrong directory.
	h := cfg.HarnessOf(rc.Context.Agent)
	cfg.ApplyDefaultProfileDir(rc.Name, &rc.Context)
	if rc.Context.IgnoredDefaultConfigDir {
		res.Notices = append(res.Notices,
			`profile "default" always uses native harness config directories; ignoring config_dir override in profiles/default.yaml (create a named profile with 'aide profile create <name>' for isolated config directories)`)
	}
	if rc.Name == config.DefaultProfileName && homeDir != "" {
		if d, ok := harness.Lookup(h); ok && d.DefaultConfigDir != "" {
			if legacyDir := harness.ProfileConfigDir(h, config.DefaultProfileName, "", homeDir); legacyDir != "" {
				if _, statErr := os.Stat(legacyDir); statErr == nil {
					res.Notices = append(res.Notices, fmt.Sprintf(
						"profile %q now uses %s; delete legacy directory %s if you no longer need it",
						config.DefaultProfileName,
						homepath.Tilde(d.DefaultDir(homeDir), homeDir),
						homepath.Tilde(legacyDir, homeDir),
					))
				}
			}
		}
	}
	env, err := provision.InjectProfileEnv(h, rc.Context, rc.Context.Env, homeDir)
	if err != nil {
		return res, fmt.Errorf("profile %q: %w", rc.Name, err)
	}
	rc.Context.Env = env
	return res, nil
}

// SelectHarness sets rc's final harness by precedence: --agent, then
// the saved repository preference, then the trusted .aide.yaml agent
// (already merged into rc by Resolve), then the profile default. It
// runs before profile-dir injection so the directory is the final
// harness's. notice is non-empty when the saved harness overrides a
// different .aide.yaml harness.
func SelectHarness(cfg *config.Config, rc *aidectx.ResolvedContext, in SelectionInputs, lookPath LookPathFunc) (sel Selection, notice string, err error) {
	sel.ProfileSource = rc.Source
	repoAgent := ""
	if po := cfg.ProjectOverride; po != nil {
		repoAgent = po.Agent
	}
	agent := ""
	switch {
	case in.Agent != "":
		agent, sel.HarnessSource = in.Agent, SourceFlag
	case in.Saved.Harness != "":
		agent, sel.HarnessSource = in.Saved.Harness, SourcePreference
		if repoAgent != "" && cfg.HarnessOf(repoAgent) != cfg.HarnessOf(agent) {
			notice = fmt.Sprintf("using harness %q saved for this repository; %s sets %q "+
				"(change with: aide project set --agent <name>, or aide project clear --agent)",
				agent, cfg.ProjectFileSetting(func(po *config.ProjectOverride) bool { return po.Agent != "" }), repoAgent)
		}
	case repoAgent != "":
		sel.HarnessSource = SourceRepository
	default:
		sel.HarnessSource = SourceProfile
	}
	if agent == "" {
		return sel, "", nil
	}
	if err := ValidateAgentOverride(cfg, agent, lookPath); err != nil {
		if sel.HarnessSource == SourcePreference {
			return sel, "", fmt.Errorf("harness %q saved for this repository: %w\n\n"+
				"Change it with: aide project set --agent <name>, or clear it with: aide project clear --agent", agent, err)
		}
		return sel, "", err
	}
	rc.Context = provision.WithAgent(rc.Context, agent, cfg.HarnessOf)
	return sel, notice, nil
}

// ResolveChoice decides which gateway harnessName launches with, by
// precedence: --gateway (or --auth login, which is --gateway none),
// the repository preference, the profile's gateway_choice (or, for
// legacy gateways, the choice today's default rule makes), no
// candidates, then asking. ctx is the profile with its legacy
// credentials translated. Harnesses without a gateway reach resolve to
// the zero Choice. A stored choice naming a gateway that is not marked
// for harnessName is reported in notices and treated as unset. When
// choices are unset, a multi harness starts on its own default; a slot
// harness asks through ask, or exits 78 when ask reports ErrNoTerminal;
// with a nil ask (which, status, prompt: passive preview) it stays
// unset and never asks or fails.
func ResolveChoice(cfg *config.Config, profile string, ctx config.Context, harnessName string, in SelectionInputs, ask ChoicePrompter) (Choice, []string, error) {
	d, ok := harness.Lookup(harnessName)
	if !ok || d.Reach == harness.ReachNone {
		return Choice{}, nil, nil
	}
	candidates := ctx.GatewayCandidates(harnessName)
	allRenames := cfg.LegacyRenames[profile]
	var notices []string
	// stored reports whether a saved choice v applies; a stale one is
	// noted and skipped.
	stored := func(v, where string) bool {
		if v == "" {
			return false
		}
		if v == gatewayNone || slices.Contains(candidates, v) {
			return true
		}
		notices = append(notices, fmt.Sprintf("%s names gateway %q, which is not marked for %s; treating the choice as unset", where, v, harnessName))
		return false
	}

	if in.Auth == AuthLogin {
		return Choice{Value: gatewayNone, Source: SourceFlag}, nil, nil
	}
	if in.CredentialsSet {
		v, err := flagChoice(ctx, harnessName, in.Credentials, allRenames[harnessName], candidates)
		if err != nil {
			return Choice{}, nil, err
		}
		return Choice{Value: v, Source: SourceFlag}, nil, nil
	}
	rec, translated := gatewaymigrate.TranslatePreference(in.Saved, ctx, allRenames)
	notices = append(notices, translated...)
	if v := rec.Gateway[harnessName]; stored(v, "the saved repository preference") {
		return Choice{Value: v, Source: SourceRepo}, notices, nil
	}
	if v := ctx.GatewayChoice[harnessName]; stored(v, fmt.Sprintf("profile %q gateway_choice", profile)) {
		return Choice{Value: v, Source: SourceProfile}, notices, nil
	}
	if v := ctx.DerivedChoice[harnessName]; v != "" {
		return Choice{Value: v, Source: SourceProfile, Derived: true}, notices, nil
	}
	if len(candidates) == 0 {
		return Choice{Value: gatewayNone, Source: SourceNoCandidates}, notices, nil
	}
	unset := Choice{Source: SourceUnset}
	if d.Reach == harness.ReachMulti || ask == nil {
		return unset, notices, nil
	}
	options := make([]ui.PickerOption, len(candidates))
	for i, name := range candidates {
		gw := ctx.ConvertedGateways[name]
		options[i] = ui.PickerOption{Name: name, URL: cmp.Or(gw.URL, gw.BaseURL), Store: gw.Store, Key: gw.Key}
	}
	v, saved, err := ask(harnessName, profile, options)
	if errors.Is(err, ErrNoTerminal) || errors.Is(err, ui.ErrPickerAborted) {
		return unset, notices, fmt.Errorf("%s has gateways marked in profile %q (%s) but none chosen; choose one with: "+
			"aide gateway use <name|none> --agent %s, or pass --gateway <name|none>: %w",
			harnessName, profile, strings.Join(candidates, ", "), harnessName, &usage.ExitError{Code: exitConfig})
	}
	if err != nil {
		return unset, notices, err
	}
	return Choice{Value: v, Source: SourcePicker, Saved: saved}, notices, nil
}

// gatewayNone is the choice that leaves the harness on its own login.
const gatewayNone = "none"

// flagChoice reads the gateway out of a --gateway selection: "none", or
// a gateway (renamed for the harness through renames, old name to
// new) that serves it. Names of ordinary credentials are not gateways
// and are left to credential planning; a selection with no gateway in
// it replaces the defaults with none.
func flagChoice(ctx config.Context, harnessName string, names []string, renames map[string]string, candidates []string) (string, error) {
	for _, n := range names {
		if n == gatewayNone {
			return gatewayNone, nil
		}
		if to, ok := renames[n]; ok {
			n = to
		}
		if slices.Contains(candidates, n) {
			return n, nil
		}
		if _, known := ctx.ConvertedGateways[n]; known {
			return "", fmt.Errorf("%w: gateway %q does not serve %s (marked for: %s)", ErrCredentialIncompatible, n, harnessName,
				strings.Join(ctx.ConvertedGateways[n].EffectiveHarnesses(), ", "))
		}
		if _, isCredential := ctx.Credentials[n]; !isCredential {
			if _, isPreset := gatewaypreset.Lookup(n); isPreset {
				return "", fmt.Errorf("%w: unknown gateway %q; add it with: aide gateway add %s", ErrCredentialNotFound, n, n)
			}
		}
	}
	return gatewayNone, nil
}

// SelectCredentials sets the credential selection by precedence:
// --credential, then the saved repository selection, then
// profileDefault. A selection replaces the lower ones; it never unions
// with them, and there is no ambient fallback. --auth login selects
// nothing; what it displaces is kept for the "not injecting" note.
func (s *Selection) SelectCredentials(in SelectionInputs, profileDefault []string) {
	switch {
	case in.CredentialsSet:
		s.requested, s.CredentialSource = in.Credentials, SourceFlag
	case in.Saved.LegacyCredentials != nil:
		s.requested, s.CredentialSource = *in.Saved.LegacyCredentials, SourcePreference
	default:
		s.requested, s.CredentialSource = profileDefault, SourceProfile
	}
	s.requested = append([]string{}, s.requested...)
	s.Credentials = s.requested
	if in.Auth == AuthLogin {
		s.Credentials, s.CredentialSource = []string{}, SourceAuthLogin
	}
}

func isGatewayName(ctx config.Context, name string) bool {
	_, converted := ctx.ConvertedGateways[name]
	_, declared := ctx.Gateways[name]
	return converted || declared
}

// dropGatewayNames removes from the credential selection the names that
// choose a gateway instead ("none", and gateways); the gateway choice
// carries them.
func (s *Selection) dropGatewayNames(ctx config.Context) {
	s.requested = slices.DeleteFunc(s.requested, func(name string) bool {
		return name == gatewayNone || isGatewayName(ctx, name)
	})
	if s.CredentialSource != SourceAuthLogin {
		s.Credentials = slices.Clone(s.requested)
	}
}

// PlanCredentials plans s's selection for profile ctx and harnessName
// (see the package-level PlanCredentials). In login mode the displaced
// selection is reported as ignored. A saved or default selection
// covers every harness the profile runs, so it skips bindings
// restricted to other harnesses; --credential stays strict.
func (s *Selection) PlanCredentials(profile string, ctx config.Context, harnessName string, auth AuthMode) (CredentialPlan, error) {
	requested, skipped := s.requested, []string(nil)
	switch s.CredentialSource {
	case SourceProfile:
		forHarnessCtx := ctx
		forHarnessCtx.DefaultCredentials = requested
		allowed := config.DefaultCredentialsForHarness(forHarnessCtx, harnessName)
		gatewayOn := s.Gateway.Value != "" && s.Gateway.Value != gatewayNone
		requested = slices.DeleteFunc(slices.Clone(requested), func(name string) bool {
			b, ok := ctx.Credentials[name]
			if !ok {
				return false
			}
			if gatewayOn && b.IsLLMProvider(name) { // the gateway holds the one LLM provider slot
				return true
			}
			if !b.DefaultAllowsHarness(name, harnessName) {
				skipped = append(skipped, name)
				return true
			}
			return !slices.Contains(allowed, name)
		})
		s.Credentials = slices.Clone(requested)
	case SourcePreference:
		requested = slices.DeleteFunc(slices.Clone(requested), func(name string) bool {
			b, ok := ctx.Credentials[name]
			if ok && !b.AllowsHarness(harnessName) {
				skipped = append(skipped, name)
				return true
			}
			return false
		})
	}
	plan, err := PlanCredentials(profile, ctx, harnessName, requested, auth)
	plan.Skipped = skipped
	return plan, s.explainSavedSelection(err)
}

// explainSavedSelection adds a remedy to a credential planning error
// caused by a saved selection.
func (s *Selection) explainSavedSelection(err error) error {
	if err == nil || s.CredentialSource != SourcePreference {
		return err
	}
	return fmt.Errorf("credentials saved for this repository: %w\n\n"+
		"Change them with: aide project set --gateway <name>, or clear them with: aide project clear --gateway", err)
}

// SetBannerSelection records sel's sources and credential selection on
// data, by name only.
func SetBannerSelection(data *ui.BannerData, sel Selection) {
	data.ProfileSource = sel.ProfileSource
	data.HarnessSource = sel.HarnessSource
	data.CredentialSource = sel.CredentialSource
	data.CredentialSelection = slices.Clone(sel.Credentials)
}

// SavedPreference returns the preference saved in the file at path for
// profile and the repository's normalized origin remote. Without an
// origin remote there is nothing to look up and it returns the zero
// preference; a missing file is not an error.
func SavedPreference(path, profile string, remotes aidectx.RemoteLookup) (prefs.Preference, error) {
	key := PreferenceKey(profile, remotes)
	if key.Remote == "" {
		return prefs.Preference{}, nil
	}
	f, err := prefs.Load(path)
	if err != nil {
		return prefs.Preference{}, err
	}
	p, _ := f.Get(key)
	return p, nil
}

// PreferenceKey is the key repository preferences for profile are
// saved and read under: the profile and the repository's normalized
// origin remote, which is "" without one (or with nil remotes).
func PreferenceKey(profile string, remotes aidectx.RemoteLookup) prefs.Key {
	key := prefs.Key{Profile: profile}
	if remotes != nil {
		key.Remote = aidectx.ParseRemoteHost(remotes(aidectx.DefaultRemoteName))
	}
	return key
}

// ProjectProfileNotice explains a .aide.yaml profile: reference that
// did not decide the profile. The reference never selects or switches
// identity; the notice suggests --profile instead. It is empty when
// there is no reference or it names the resolved profile.
func ProjectProfileNotice(cfg *config.Config, rc *aidectx.ResolvedContext) string {
	po := cfg.ProjectOverride
	if po == nil || po.Profile == "" || po.Profile == rc.Name {
		return ""
	}
	file := cfg.ProjectFileSetting(func(po *config.ProjectOverride) bool { return po.Profile != "" })
	if _, ok := cfg.Contexts[po.Profile]; !ok {
		defined := strings.Join(slices.Sorted(maps.Keys(cfg.Contexts)), ", ")
		if defined == "" {
			defined = "none"
		}
		return fmt.Sprintf("%s references profile %q, which your config does not define (defined: %s); "+
			"using profile %q. A repository file never selects a profile: define it under profiles: "+
			"in your aide config, or pick one with --profile <name>",
			file, po.Profile, defined, rc.Name)
	}
	return fmt.Sprintf("%s suggests profile %q, but this launch uses profile %q; "+
		"a repository file never switches identity, so to use it run: aide --profile %s",
		file, po.Profile, rc.Name, po.Profile)
}
