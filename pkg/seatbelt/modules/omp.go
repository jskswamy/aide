package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// OmpAgent returns a module with Oh My Pi (omp) CLI agent sandbox rules.
//
// Default state lives under ~/.omp (agent state in ~/.omp/agent, plus
// ~/.omp/{natives,logs,run,plugins}). When PI_CODING_AGENT_DIR is set
// (every OMP profile launch), the config grant narrows to that directory
// alone so one profile cannot read ~/.omp/agent/agent.db, ~/.omp/.env,
// ~/.omp/auth-broker.token, or another profile's directory. The
// non-credential ~/.omp/{natives,logs,run} paths remain granted on
// overridden launches so native addon extraction and fallback runtime
// directories work even when PI_CONFIG_DIR is not set.
func OmpAgent() seatbelt.Module {
	return ompModule{
		config: NewSimpleAgent(AgentSpec{
			DisplayName:     "OMP Agent",
			SectionName:     "OMP",
			EnvKey:          "PI_CODING_AGENT_DIR",
			HomeRelDefaults: []string{".omp"},
		}),
		runtime: NewSimpleAgent(AgentSpec{
			DisplayName: "OMP Agent",
			SectionName: "OMP runtime",
			HomeRelDefaults: []string{
				".omp/natives",
				".omp/logs",
				".omp/run",
			},
		}),
	}
}

type ompModule struct {
	config  seatbelt.Module
	runtime seatbelt.Module
}

func (m ompModule) Name() string { return m.config.Name() }

func (m ompModule) AgentEnvKeys(ctx *seatbelt.Context) []string {
	if kp, ok := m.config.(seatbelt.EnvKeyProvider); ok {
		return kp.AgentEnvKeys(ctx)
	}
	return nil
}

func (m ompModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	r := m.config.Rules(ctx)
	if ctx != nil {
		if dir, ok := ctx.EnvLookup("PI_CODING_AGENT_DIR"); ok && dir != "" {
			rt := m.runtime.Rules(ctx)
			r.Rules = append(r.Rules, rt.Rules...)
			r.Writable = append(r.Writable, rt.Writable...)
			r.Readable = append(r.Readable, rt.Readable...)
			r.Skipped = append(r.Skipped, rt.Skipped...)
			r.Hints = append(r.Hints, rt.Hints...)
		}
	}
	return r
}
