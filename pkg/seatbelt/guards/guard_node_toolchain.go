// Node toolchain guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/30-toolchains/node.sb

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type nodeToolchainGuard struct{}

// NodeToolchainGuard returns a Guard with Node.js ecosystem sandbox rules.
func NodeToolchainGuard() seatbelt.Guard { return &nodeToolchainGuard{} }

func (g *nodeToolchainGuard) Name() string        { return "node-toolchain" }
func (g *nodeToolchainGuard) Type() string        { return "always" }
func (g *nodeToolchainGuard) Description() string { return "Node.js, npm, yarn, pnpm, and browser testing tool paths" }

func (g *nodeToolchainGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// Node version managers
		seatbelt.Section("Node version managers"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nvm") + `
    ` + seatbelt.HomeSubpath(home, ".fnm") + `
)`),

		// npm
		seatbelt.Section("npm"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".npm") + `
    ` + seatbelt.HomeSubpath(home, ".config/npm") + `
    ` + seatbelt.HomeSubpath(home, ".cache/npm") + `
    ` + seatbelt.HomeSubpath(home, ".cache/node") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/npm") + `
    ` + seatbelt.HomeLiteral(home, ".npmrc") + `
    ` + seatbelt.HomeSubpath(home, ".config/configstore") + `
    ` + seatbelt.HomeSubpath(home, ".node-gyp") + `
    ` + seatbelt.HomeSubpath(home, ".cache/node-gyp") + `
)`),

		// pnpm
		seatbelt.Section("pnpm"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".config/pnpm") + `
    ` + seatbelt.HomeSubpath(home, ".pnpm-state") + `
    ` + seatbelt.HomeSubpath(home, ".pnpm-store") + `
    ` + seatbelt.HomeSubpath(home, ".local/share/pnpm") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/pnpm") + `
    ` + seatbelt.HomeSubpath(home, "Library/Preferences/pnpm") + `
)`),

		// yarn
		seatbelt.Section("yarn"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".yarn") + `
    ` + seatbelt.HomeLiteral(home, ".yarnrc") + `
    ` + seatbelt.HomeLiteral(home, ".yarnrc.yml") + `
    ` + seatbelt.HomeSubpath(home, ".config/yarn") + `
    ` + seatbelt.HomeSubpath(home, ".cache/yarn") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/Yarn") + `
)`),

		// corepack
		seatbelt.Section("corepack"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/node/corepack") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/node/corepack") + `
)`),

		// Browser testing and tools
		seatbelt.Section("Browser testing and tools"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Caches/ms-playwright") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/Cypress") + `
    ` + seatbelt.HomeSubpath(home, ".cache/puppeteer") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/typescript") + `
)`),

		// Prisma
		seatbelt.Section("Prisma"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/prisma") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/prisma-nodejs") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/checkpoint-nodejs") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/claude-cli-nodejs") + `
)`),

		// Turborepo
		seatbelt.Section("Turborepo"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".cache/turbo") + `
    ` + seatbelt.HomeSubpath(home, "Library/Caches/turbo") + `
    ` + seatbelt.HomeSubpath(home, "Library/Application Support/turborepo") + `
)`),
	}
}
