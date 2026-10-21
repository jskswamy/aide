## Unreleased

<!--
Add release notes here as specs ship. Group under the headings
below. When tagging, replace `## Unreleased` with `## vX.Y.Z`,
commit, then tag.

Headings (use only what applies; drop empty sections):
  ### 💥 Breaking changes
  ### ✨ New
  ### 🔧 Internal

After tagging, reset this file to this template.
-->

### 🔧 Internal

- Fixed OpenCode yolo argument placement for subcommands. OpenCode v2 strictly
  requires flags to appear after subcommands (e.g. `opencode run --auto`),
  rejecting `--auto` when prepended before subcommands. aide now inserts `--auto`
  after `run` and omits it for subcommands that do not accept it.
