## v3.6.0 (2026-11-07)

### ✨ New

- Discover all configured `age` recipients across `SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE`, and default `keys.txt` paths with an interactive recipient picker when multiple keys exist and in-process X25519 key generation when no key is found.
- Prompt interactively on TTY launches when an untrusted `.aide.yaml` file is encountered (`[y] Trust this file / [s] Skip for this launch / [d] Deny permanently`), displaying the requested capabilities, agent, MCP servers, and sandbox overrides before applying or rejecting the file.
