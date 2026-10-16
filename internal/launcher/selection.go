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
// harness_source and credential_source.
const (
	SourceFlag       = "flag"       // --agent, --credential
	SourceAuthLogin  = "auth"       // --auth login: nothing injected
	SourcePreference = "preference" // saved with aide project set
	SourceRepository = "repository" // trusted .aide.yaml agent:
	SourceProfile    = "profile"    // the profile's agent: / default_credentials:
	// SourcePrompt is a profile picked at the empty-state prompt.
	SourcePrompt = "prompt"
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
				agent, cfg.ProjectConfigPath, repoAgent)
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
	rc.Context = provision.WithAgent(rc.Context, agent)
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
// selection is reported as ignored.
func (s *Selection) PlanCredentials(profile string, ctx config.Context, harnessName string, auth AuthMode) (CredentialPlan, error) {
	plan, err := PlanCredentials(profile, ctx, harnessName, s.requested, auth)
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
	if remotes == nil {
		return prefs.Preference{}, nil
	}
	remote := aidectx.ParseRemoteHost(remotes(aidectx.DefaultRemoteName))
	if remote == "" {
		return prefs.Preference{}, nil
	}
	f, err := prefs.Load(path)
	if err != nil {
		return prefs.Preference{}, err
	}
	p, _ := f.Get(prefs.Key{Profile: profile, Remote: remote})
	return p, nil
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
	if _, ok := cfg.Contexts[po.Profile]; !ok {
		defined := strings.Join(slices.Sorted(maps.Keys(cfg.Contexts)), ", ")
		if defined == "" {
			defined = "none"
		}
		return fmt.Sprintf("%s references profile %q, which your config does not define (defined: %s); "+
			"using profile %q. A repository file never selects a profile: define it under profiles: "+
			"in your aide config, or pick one with --profile <name>",
			cfg.ProjectConfigPath, po.Profile, defined, rc.Name)
	}
	return fmt.Sprintf("%s suggests profile %q, but this launch uses profile %q; "+
		"a repository file never switches identity, so to use it run: aide --profile %s",
		cfg.ProjectConfigPath, po.Profile, rc.Name, po.Profile)
}
