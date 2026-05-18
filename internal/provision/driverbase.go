package provision

// Capabilities is the static capability/identity block every
// Provisioner driver carries. Drivers populate it once in their
// constructor; DriverBase promotes it to the five trivial methods
// Provisioner requires (Name, SupportsPlugins, SupportsMCP,
// RequiresTTY, SupportedSourceShapes).
//
// Adding a new capability bit (e.g. SupportsHooks) means extending
// this struct and DriverBase once, rather than touching every
// per-agent package.
type Capabilities struct {
	AgentName       string
	SupportsPlugins bool
	SupportsMCP     bool
	RequiresTTY     bool
	SourceShapes    []SourceShape
	// ProfileEnvKey is the env-var name the driver injects when a
	// context declares profile:. Empty string signals the driver does
	// not support profile (see DriverBase.Profile, which returns
	// ErrProfileNotSupported in that case).
	ProfileEnvKey string
}

// DriverBase is embeddable in per-agent Driver structs. It carries
// the Capabilities and promotes the five trivial Provisioner methods
// that previously lived as one-line stubs in every driver.
type DriverBase struct {
	Caps Capabilities
}

// Name implements Provisioner.
func (d DriverBase) Name() string { return d.Caps.AgentName }

// SupportsPlugins implements Provisioner.
func (d DriverBase) SupportsPlugins() bool { return d.Caps.SupportsPlugins }

// SupportsMCP implements Provisioner.
func (d DriverBase) SupportsMCP() bool { return d.Caps.SupportsMCP }

// RequiresTTY implements Provisioner.
func (d DriverBase) RequiresTTY() bool { return d.Caps.RequiresTTY }

// SupportedSourceShapes implements Provisioner.
func (d DriverBase) SupportedSourceShapes() []SourceShape { return d.Caps.SourceShapes }
