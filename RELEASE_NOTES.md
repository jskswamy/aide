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

### ✨ New

- Split `~/.config/aide/` global configuration into modular files (`config.yaml` for global settings, `sandbox.yaml`, `provisioning.yaml`, and `profiles/<name>.yaml` per profile) while preserving backward-compatible loading for legacy single-file configs, scatter-writing only modified files, and adding `aide config migrate` to split single-file configs into the modular layout.
