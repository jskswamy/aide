## Unreleased

<!--
Add release notes here as specs ship. Group under the headings
below. When tagging, replace `## Unreleased` with `## vX.Y.Z`,
commit, then tag.

Headings (use only what applies; drop empty sections):
  ### 💥 Breaking changes
  ### ✨ New
  ### 🐛 Bug Fixes
  ### 🔧 Internal

After tagging, reset this file to this template.
-->

### 🐛 Bug Fixes

- Emit 2-space indented YAML across all config writers (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, `profiles/<name>.yaml`, and `.aide.yaml`), preserve comments, `---` headers, and key order when updating existing YAML files, and skip writing split files whose semantic contents did not change.
