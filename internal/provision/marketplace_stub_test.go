package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
	"github.com/jskswamy/aide/internal/provision/agents/opencode"
	pidriver "github.com/jskswamy/aide/internal/provision/agents/pi"
)

// URL-direct-only agents have no marketplaces: adding one is a user
// error, and removing one is a no-op that install rollback relies on.
func TestURLDirectDriversMarketplaceStubs(t *testing.T) {
	for _, tt := range []struct {
		prov    provision.Provisioner
		wantErr string
	}{
		{pidriver.New(nil), "pi does not have marketplaces; declare packages inline with string values"},
		{opencode.New(nil), "opencode does not have marketplaces; declare plugins inline with string values"},
		{gemini.New(nil), "gemini does not have marketplaces; declare extensions inline with string values"},
	} {
		t.Run(tt.prov.Name(), func(t *testing.T) {
			err := tt.prov.AddMarketplace(provision.Context{}, provision.Marketplace{Key: "a/b"})
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("AddMarketplace error = %v, want %q", err, tt.wantErr)
			}
			if err := tt.prov.RemoveMarketplace(provision.Context{}, "a/b"); err != nil {
				t.Errorf("RemoveMarketplace = %v, want nil", err)
			}
			if got, err := tt.prov.InstalledMarketplaces(provision.Context{}); got != nil || err != nil {
				t.Errorf("InstalledMarketplaces = %v, %v, want nil, nil", got, err)
			}
		})
	}
}

// Drivers that declare marketplace sources but don't implement them
// keep the generic unsupported errors.
func TestDriverBaseMarketplaceStubsUnsupported(t *testing.T) {
	d := provision.DriverBase{Caps: provision.Capabilities{
		AgentName:    "x",
		SourceShapes: []provision.SourceShape{provision.ShapeMarketplace},
	}}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil || err.Error() != "x: marketplaces not supported" {
		t.Errorf("AddMarketplace = %v", err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "m"); err == nil || err.Error() != "x: marketplaces not supported" {
		t.Errorf("RemoveMarketplace = %v", err)
	}
}
