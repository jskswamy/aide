## v1.14.1 — 2026-06-04

### ✨ Features

- **`aide prompt` suppresses context name when an icon is set.**
  When a context has an `icon:` field configured, `aide prompt` now
  shows the icon *in place of* the text name, eliminating redundant
  output like `default 🏠 🤖 🛡` in favour of the cleaner `🏠 🤖 🛡`.
  Contexts that have no `icon:` field (or whose icon sanitises to an
  empty string) continue to display the name unchanged — fully
  backwards-compatible.

- **`aide prompt --compact` flag for space-free prompt output.**
  A new `--compact` flag joins all prompt segments without spaces,
  producing `🏠🤖🛡` instead of `🏠 🤖 🛡`. Useful for tighter
  Starship right-prompt configurations where every character counts.

  Example Starship snippet for compact mode:

  ```toml
  [custom.aide]
  command = "aide prompt --compact"
  when = "true"
  symbol = ""
  timeout = 100
  ```
