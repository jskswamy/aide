## v3.6.3 (2026-11-14)

### 🐛 Bug Fixes

- Restore the previous gateway token when `aide gateway add --force` or `aide gateway edit` fails to write the config. Before, `add --force` deleted the key it had just overwritten (and the whole store file when it was the last key), and `edit` left the new token in place while the config still described the old gateway.
- Refuse a credential selection that would send one credential's key to another credential's endpoint, such as the built-in `anthropic` key next to a custom route that sets `ANTHROPIC_BASE_URL`. Claude used to start with the custom URL and the direct Anthropic key. This applies to `--credential`, saved repository selections and `default_credentials`; the error names both credentials. Gateways were not affected.
- List `omp` in the default harnesses named by the `--harnesses` help of `aide gateway add` and `aide gateway edit`.
