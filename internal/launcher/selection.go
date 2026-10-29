package launcher

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/ui"
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
	} else if res.Gate, err = l.applyTrustGate(cfg); err != nil {
		return res, err
	}
	remotes := aidectx.GitRemotes(cwd)
	if res.RC, err = aidectx.ResolveSelected(cfg, cwd, remotes, l.Profile); err != nil {
		if l.Profile != "" || cfg.DefaultContext != "" || l.EmptyStateActions == nil {
			return res, fmt.Errorf("resolving profile: %w", err)
		}
		if res.RC, cfg, err = l.handleEmptyStateLaunch(cfg, cwd, remotes); err != nil {
			return res, err
		}
		res.Config = cfg
		// The reloaded config was gated; refresh what the banner and
		// AIDE_TRUST report from it (the gate is idempotent).
		if !l.IgnoreProjectConfig {
			if res.Gate, err = l.applyTrustGate(cfg); err != nil {
				return res, err
			}
		}
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
	env, err := provision.InjectProfileEnv(h, rc.Context, rc.Context.Env, homeDir)
	if err != nil {
		return res, fmt.Errorf("profile %q: %w", rc.Name, err)
	}
	rc.Context.Env = env
	res.Refused = cfg.GlobalLoginError(rc.Name, h, homeDir)
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

// SelectCredentials sets the credential selection by precedence:
// --credential, then the saved repository selection, then
// profileDefault. A selection replaces the lower ones; it never unions
// with them, and there is no ambient fallback. --auth login selects
// nothing; what it displaces is kept for the "not injecting" note.
func (s *Selection) SelectCredentials(in SelectionInputs, profileDefault []string) {
	switch {
	case in.CredentialsSet:
		s.requested, s.CredentialSource = in.Credentials, SourceFlag
	case in.Saved.Credentials != nil:
		s.requested, s.CredentialSource = *in.Saved.Credentials, SourcePreference
	default:
		s.requested, s.CredentialSource = profileDefault, SourceProfile
	}
	s.requested = append([]string{}, s.requested...)
	s.Credentials = s.requested
	if in.Auth == AuthLogin {
		s.Credentials, s.CredentialSource = []string{}, SourceAuthLogin
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
		requested = slices.DeleteFunc(slices.Clone(requested), func(name string) bool {
			b, ok := ctx.Credentials[name]
			if !ok {
				return false
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
		"Change them with: aide project set --credential <name>, or clear them with: aide project clear --credential", err)
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
