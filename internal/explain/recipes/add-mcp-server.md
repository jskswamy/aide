# Add an MCP server

If your config still uses the legacy `contexts:` key, put these entries
under `contexts:` instead (same fields); a file cannot mix the two.
`aide profile migrate` converts it.

Top-level `mcp_servers` are included in **every** profile by default.
To restrict a server to one profile, define it inside that profile's
`extra` block instead. `env:` values support `{{ .secrets.<name> }}`
templating; prefer that over inline literal tokens.

## Add to all profiles (top-level)

    mcp_servers:
      github:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
        env:
          GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

## Add to one profile only (profile-scoped extra)

    profiles:
      work:
        mcp_servers:
          extra:
            github:
              command: npx
              args: ["-y", "@modelcontextprotocol/server-github"]
              env:
                GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

## Add top-level but exclude from some profiles

    mcp_servers:
      github:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
        env:
          GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

    profiles:
      personal:
        mcp_servers:
          exclude: [github]
