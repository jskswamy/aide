package claude_test

// Claude's MCP path is CLI-driven via the MCPInstaller methods
// (see mcp.go). MCPHandler intentionally returns nil — the
// MCPConfigPathEmpty test in claude_test.go pins that contract.
// This file is kept as a placeholder to avoid breaking imports;
// it can be deleted once no tooling references it.
