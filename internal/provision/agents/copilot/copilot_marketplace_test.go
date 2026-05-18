package copilot_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/copilot"
)

func TestCopilotMCPHandler(t *testing.T) {
	d := copilot.New(&fakeRunner{})
	if d.MCPHandler(provision.Context{}) == nil {
		t.Error("MCPHandler must not be nil")
	}
}

func TestCopilotInstalledMarketplacesParses(t *testing.T) {
	stdout := `NAME          SOURCE
acme          github:acme/copilot-marketplace
zeta          github:zeta/copilot-marketplace
`
	r := &fakeRunner{stdout: stdout}
	d := copilot.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	want := []provision.Marketplace{
		{Name: "acme", Source: "github:acme/copilot-marketplace", Key: "github:acme/copilot-marketplace"},
		{Name: "zeta", Source: "github:zeta/copilot-marketplace", Key: "github:zeta/copilot-marketplace"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestCopilotInstalledMarketplacesEmptyHeader(t *testing.T) {
	r := &fakeRunner{stdout: "No marketplaces configured.\n"}
	d := copilot.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v want empty", got)
	}
}

func TestCopilotInstalledMarketplacesRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("binary missing")}
	d := copilot.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Errorf("runner error must collapse to nil, got %v", err)
	}
	if got != nil {
		t.Errorf("got %v want nil on runner error", got)
	}
}

func TestCopilotInstalledMarketplacesNonZeroExit(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "unknown subcommand"}
	d := copilot.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Errorf("non-zero exit should collapse, got %v", err)
	}
	if got != nil {
		t.Errorf("got %v want nil", got)
	}
}

func TestCopilotAddMarketplaceUsesSource(t *testing.T) {
	r := &fakeRunner{}
	d := copilot.New(r)
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{
		Key:    "acme/repo",
		Name:   "acme",
		Source: "github:acme/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"copilot", "plugin", "marketplace", "add", "github:acme/repo"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestCopilotAddMarketplaceFallsBackToKey(t *testing.T) {
	r := &fakeRunner{}
	d := copilot.New(r)
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{Key: "acme/repo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"copilot", "plugin", "marketplace", "add", "acme/repo"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestCopilotAddMarketplaceFailure(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "network unreachable"}
	d := copilot.New(r)
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{Source: "github:a/b"}); err == nil {
		t.Error("expected error on non-zero exit")
	}
}

func TestCopilotRemoveMarketplaceInvokes(t *testing.T) {
	r := &fakeRunner{}
	d := copilot.New(r)
	if err := d.RemoveMarketplace(provision.Context{}, "acme"); err != nil {
		t.Fatal(err)
	}
	want := []string{"copilot", "plugin", "marketplace", "remove", "acme"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestCopilotRemoveMarketplaceMissingIsOK(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "marketplace 'x' not configured"}
	d := copilot.New(r)
	if err := d.RemoveMarketplace(provision.Context{}, "x"); err != nil {
		t.Errorf("missing marketplace must be tolerated: %v", err)
	}
}

func TestCopilotParsePluginListHandlesNoPluginsHeader(t *testing.T) {
	r := &fakeRunner{stdout: "No plugins installed.\n"}
	d := copilot.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
}

func TestCopilotInstalledPluginsRunnerError(t *testing.T) {
	r := &fakeRunner{err: errors.New("not on PATH")}
	d := copilot.New(r)
	if _, err := d.InstalledPlugins(provision.Context{}); err == nil {
		t.Error("expected error when runner errors")
	}
}

func TestCopilotInstalledPluginsNonZeroExit(t *testing.T) {
	r := &fakeRunner{code: 2, stderr: "auth required"}
	d := copilot.New(r)
	if _, err := d.InstalledPlugins(provision.Context{}); err == nil {
		t.Error("expected error on non-zero exit")
	}
}
