## Unreleased

### ✨ New

- Allow adding a loopback gateway without a bearer token when `/v1/models` returns 200 with no `Authorization` header. Persist an empty store/key only for localhost; remote hosts still need a token. Launch, sync, and harness writers omit bearer wiring when no key is stored.
