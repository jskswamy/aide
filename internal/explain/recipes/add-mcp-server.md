# Add an MCP server

Top-level `mcp_servers` are included in **every** context by default.
To restrict a server to one context, define it inside that context's
`extra` block instead. `env:` values support `{{ .secrets.<name> }}`
templating; prefer that over inline literal tokens.

## Add to all contexts (top-level)

    mcp_servers:
      github:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
        env:
          GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

## Add to one context only (context-scoped extra)

    contexts:
      work:
        mcp_servers:
          extra:
            github:
              command: npx
              args: ["-y", "@modelcontextprotocol/server-github"]
              env:
                GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

## Add top-level but exclude from some contexts

    mcp_servers:
      github:
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
        env:
          GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"

    contexts:
      personal:
        mcp_servers:
          exclude: [github]
