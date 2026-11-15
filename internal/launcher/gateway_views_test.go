package launcher

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/ui"
)

// viewCtx marks a (claude, pi), b keyless (claude) and c (codex), with
// pi chosen as a.
func viewCtx(agent string) config.Context {
	ctx := choiceCtx(map[string]string{"pi": "a"})
	ctx.Agent = agent
	return ctx
}

func TestBuildGatewayViews_OnePerHarness(t *testing.T) {
	cfg := &config.Config{}
	rc := &aidectx.ResolvedContext{Name: "work", Context: viewCtx("claude")}
	sel := Selection{}
	ctx, _, _, err := PlanProfileCredentials(cfg, rc, SelectionInputs{}, &sel)
	if err != nil {
		t.Fatal(err)
	}
	rc.Context = ctx

	byHarness := map[string]ui.CredentialInfo{}
	for _, v := range BuildGatewayViews(rc, sel.Choices) {
		byHarness[v.Harness] = v
	}
	if len(byHarness) != 5 {
		t.Fatalf("views = %d, want one per gateway-capable harness (5)", len(byHarness))
	}
	// claude is unset with candidates a and b; codex has c but is unset
	// too; pi starts on a with its key delivered as AIDE_GW_A_KEY.
	if v := byHarness["claude"]; v.Choice != "unset" || v.Source != SourceUnset {
		t.Errorf("claude = %+v, want unset", v)
	}
	pi := byHarness["pi"]
	if pi.Choice != "a" || pi.Source != SourceProfile || pi.KeyEnv != "AIDE_GW_A_KEY" || pi.Store != "s" || pi.Key != "k" || !pi.Startup || pi.Others != 0 || pi.URL != "https://a.example.com" {
		t.Errorf("pi = %+v", pi)
	}
	if v := byHarness["omp"]; v.Source != SourceNoCandidates || v.Choice != "none" {
		t.Errorf("omp = %+v, want no-candidates", v)
	}
}

func TestEnrichBanner_GatewayFields(t *testing.T) {
	cfg := &config.Config{}
	rc := &aidectx.ResolvedContext{Name: "work", Context: viewCtx("claude")}
	in := SelectionInputs{Credentials: []string{"b"}, CredentialsSet: true}
	sel := Selection{}
	ctx, _, plan, err := PlanProfileCredentials(cfg, rc, in, &sel)
	if err != nil {
		t.Fatal(err)
	}
	rc.Context = ctx
	data := &ui.BannerData{}
	SetBannerCredentials(data, plan)
	EnrichBannerHarnessesAndGateways(data, cfg, rc, "claude", "", nil, sel.Choices)

	if data.Harness != "claude" || len(data.Credentials) != 1 {
		t.Fatalf("data = %+v", data)
	}
	c := data.Credentials[0]
	if !c.IsGateway || c.Kind != "keyless" || c.Choice != "b" || c.Source != SourceFlag || c.KeyEnv != "" || c.Startup {
		t.Errorf("credential = %+v", c)
	}
	// The other claude gateway is what --gateway can switch to.
	if len(data.AvailableGateways) != 1 || data.AvailableGateways[0].Name != "a" || data.AvailableGateways[0].KeyEnv != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("available = %+v", data.AvailableGateways)
	}
	// --gateway only affects the launched harness: others show their own
	// choice without a flag warning.
	for _, v := range data.GatewayChoices {
		if v.Harness == "codex" && (len(v.Warnings) > 0 || v.Source == SourceFlag) {
			t.Errorf("codex view reflects --gateway b: %+v", v)
		}
	}
}

func TestPlanProfileCredentials_StaleChoiceIsWarningOnItsHarness(t *testing.T) {
	cfg := &config.Config{}
	ctx := choiceCtx(map[string]string{"codex": "a"}) // a is not marked for codex
	ctx.Agent = "claude"
	rc := &aidectx.ResolvedContext{Name: "work", Context: ctx}
	sel := Selection{}
	if _, _, _, err := PlanProfileCredentials(cfg, rc, SelectionInputs{}, &sel); err != nil {
		t.Fatal(err)
	}
	if got := sel.Choices["codex"]; len(got.Notices) != 1 {
		t.Errorf("codex notices = %v, want the stale-choice warning", got.Notices)
	}
}
