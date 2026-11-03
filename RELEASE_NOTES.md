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

### 💥 Breaking changes

- Replace `--path` and `--remote` on `aide profile bind` with `--scope <value>` (`repo`, `org`, `path`, or a parent git remote group/team prefix such as `gitlab.com/acme/payments/*`).

### ✨ New

- Split `~/.config/aide/` global configuration into modular files (`config.yaml` for global settings, `sandbox.yaml`, `provisioning.yaml`, and `profiles/<name>.yaml` per profile) while preserving backward-compatible loading for legacy single-file configs, scatter-writing only modified files, and adding `aide config migrate` to split single-file configs into the modular layout.
- Prompt for git remote scope selection (exact repository, parent team/subgroup prefix, organization glob, or local folder path) across `aide profile bind`, `aide profile create`, and the empty-state prompt on interactive terminals, with dynamic shell tab-completion for `--scope`.
- Detect and resolve cross-profile match rule conflicts (exact collisions, higher-specificity shadowing rules at the current directory, and narrower overlapping rules on other profiles) interactively before saving profile bindings, and automatically prune redundant narrower remote rules within the same profile.
