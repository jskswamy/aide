package provision

import (
	"reflect"
	"testing"
)

func TestDriverBasePromotesCapabilities(t *testing.T) {
	d := DriverBase{Caps: Capabilities{
		AgentName:       "x",
		SupportsPlugins: true,
		SupportsMCP:     false,
		RequiresTTY:     true,
		SourceShapes:    []SourceShape{ShapeMarketplace, ShapeURLDirect},
	}}
	if d.Name() != "x" {
		t.Errorf("Name = %q, want x", d.Name())
	}
	if !d.SupportsPlugins() {
		t.Errorf("SupportsPlugins = false")
	}
	if d.SupportsMCP() {
		t.Errorf("SupportsMCP = true, want false")
	}
	if !d.RequiresTTY() {
		t.Errorf("RequiresTTY = false")
	}
	want := []SourceShape{ShapeMarketplace, ShapeURLDirect}
	if got := d.SupportedSourceShapes(); !reflect.DeepEqual(got, want) {
		t.Errorf("SupportedSourceShapes = %v, want %v", got, want)
	}
}
