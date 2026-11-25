package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/trust"
	"github.com/jskswamy/aide/schemas"
)

type schemaExportResult struct {
	Dir   string            `json:"dir"`
	Files map[string]string `json:"files"`
}

type schemaStampEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // "stamped", "updated", "skipped", "unchanged"
	Target string `json:"target,omitempty"`
}

type schemaStampResult struct {
	Entries []schemaStampEntry `json:"entries"`
}

func configSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Inspect, export, or stamp JSON Schemas for aide YAML files",
	}
	cmd.AddCommand(configSchemaShowCmd())
	cmd.AddCommand(configSchemaExportCmd())
	cmd.AddCommand(configSchemaStampCmd())
	return cmd
}

func configSchemaShowCmd() *cobra.Command {
	validArgs := make([]string, 0, len(schemas.AllKinds()))
	for _, k := range schemas.AllKinds() {
		validArgs = append(validArgs, string(k))
	}
	return &cobra.Command{
		Use:          "show <kind>",
		Short:        "Print an embedded JSON Schema to stdout",
		Long:         fmt.Sprintf("Print an embedded JSON Schema to stdout. Valid kinds: %s.", strings.Join(validArgs, ", ")),
		Args:         cobra.ExactArgs(1),
		ValidArgs:    validArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, ok := schemas.ParseKind(args[0])
			if !ok {
				return fmt.Errorf("unknown schema kind %q (valid: %s)", args[0], strings.Join(validArgs, ", "))
			}
			data, err := schemas.Bytes(kind)
			if err != nil {
				return fmt.Errorf("reading embedded schema: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}

func configSchemaExportCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:          "export",
		Short:        "Export all embedded JSON Schemas to a local directory",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.FromCmd(cmd)
			if err != nil {
				return err
			}
			targetDir, err := resolveSchemaExportDir(dir)
			if err != nil {
				return err
			}
			written, err := exportAllSchemas(targetDir)
			if err != nil {
				return err
			}
			res := schemaExportResult{Dir: targetDir, Files: written}
			return output.Emit(cmd.OutOrStdout(), format, res, func(w io.Writer) error {
				fmt.Fprintf(w, "Exported %d schemas to %s\n", len(written), targetDir)
				for _, k := range schemas.AllKinds() {
					fmt.Fprintf(w, "  %s -> %s\n", k, written[string(k)])
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Target directory (default: ~/.config/aide/schemas)")
	return cmd
}

func configSchemaStampCmd() *cobra.Command {
	var (
		local   bool
		dir     string
		project bool
		force   bool
	)
	cmd := &cobra.Command{
		Use:          "stamp",
		Short:        "Add yaml-language-server $schema headers to existing config files",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.FromCmd(cmd)
			if err != nil {
				return err
			}

			var localFiles map[string]string
			if local {
				targetDir, derr := resolveSchemaExportDir(dir)
				if derr != nil {
					return derr
				}
				localFiles, derr = exportAllSchemas(targetDir)
				if derr != nil {
					return derr
				}
			}

			type targetFile struct {
				rel       string
				path      string
				kind      schemas.Kind
				isProject bool
			}

			cfgDir := config.Dir()
			cfgPath := config.FilePath()

			var targets []targetFile
			var globalRels []string
			if fileExists(cfgPath) {
				targets = append(targets, targetFile{rel: "config.yaml", path: cfgPath, kind: schemas.KindConfig})
				globalRels = append(globalRels, "config.yaml")
			}
			for _, rel := range config.SplitRelativePaths(cfgDir) {
				full := filepath.Join(cfgDir, filepath.FromSlash(rel))
				targets = append(targets, targetFile{rel: rel, path: full, kind: schemas.KindForSplitRel(rel)})
				globalRels = append(globalRels, rel)
			}
			if project {
				cwd, cerr := os.Getwd()
				if cerr != nil {
					return fmt.Errorf("getting working directory: %w", cerr)
				}
				disc := config.DiscoverProject(cwd)
				if disc.LocalPath != "" && fileExists(disc.LocalPath) {
					targets = append(targets, targetFile{
						path:      disc.LocalPath,
						kind:      schemas.KindProject,
						isProject: true,
					})
				}
			}

			globalChanged := false
			beforeSnapshots := make(map[string][]byte, len(globalRels))
			afterSnapshots := make(map[string][]byte, len(globalRels))
			res := schemaStampResult{Entries: make([]schemaStampEntry, 0, len(targets))}
			for _, tf := range targets {
				schemaTarget := schemas.DefaultURL(tf.kind)
				if local {
					schemaTarget = schemas.FileURI(localFiles[string(tf.kind)])
				}
				entry, before, after, serr := stampSingleFile(tf.path, tf.kind, schemaTarget, force, tf.isProject)
				if serr != nil {
					return serr
				}
				if !tf.isProject {
					beforeSnapshots[tf.rel] = before
					afterSnapshots[tf.rel] = after
					if entry.Status == "stamped" || entry.Status == "updated" {
						globalChanged = true
					}
				}
				res.Entries = append(res.Entries, entry)
			}

			if globalChanged && len(globalRels) > 0 {
				oldHash := provision.HashConfigFiles(globalRels, beforeSnapshots)
				newHash := provision.HashConfigFiles(globalRels, afterSnapshots)
				if oldHash != "" && newHash != oldHash {
					if home, hErr := os.UserHomeDir(); hErr == nil && home != "" {
						if _, rErr := provision.RehashConfig(provision.DefaultStatePath(home), oldHash, newHash); rErr != nil {
							return fmt.Errorf("stamped config files, but updating provisioning state failed: %w", rErr)
						}
					}
				}
			}

			return output.Emit(cmd.OutOrStdout(), format, res, func(w io.Writer) error {
				if len(res.Entries) == 0 {
					fmt.Fprintln(w, "No existing aide config files found to stamp.")
					return nil
				}
				for _, e := range res.Entries {
					fmt.Fprintf(w, "%-9s %s\n", e.Status, e.Path)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "Export schemas locally and stamp file:// URIs instead of GitHub URLs")
	cmd.Flags().StringVar(&dir, "dir", "", "Local schema export directory when used with --local (default: ~/.config/aide/schemas)")
	cmd.Flags().BoolVar(&project, "project", false, "Also stamp the current project's .aide.yaml if present")
	cmd.Flags().BoolVar(&force, "force", false, "Replace existing # yaml-language-server: directives")
	return cmd
}

func resolveSchemaExportDir(dir string) (string, error) {
	if dir == "" {
		dir = filepath.Join(config.Dir(), "schemas")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving schema directory: %w", err)
	}
	return abs, nil
}

func exportAllSchemas(dir string) (map[string]string, error) {
	written := make(map[string]string, len(schemas.AllKinds()))
	for _, k := range schemas.AllKinds() {
		data, err := schemas.Bytes(k)
		if err != nil {
			return nil, fmt.Errorf("reading %s schema: %w", k, err)
		}
		dest := filepath.Join(dir, schemas.Filename(k))
		if err := fsutil.AtomicWrite(dest, data); err != nil {
			return nil, fmt.Errorf("writing %s: %w", dest, err)
		}
		written[string(k)] = dest
	}
	return written, nil
}

func stampSingleFile(path string, kind schemas.Kind, target string, force, isProject bool) (schemaStampEntry, []byte, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schemaStampEntry{}, nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	entry := schemaStampEntry{Path: path, Kind: string(kind), Target: target}

	had := schemas.HasModeline(data)
	if had && !force {
		entry.Status = "skipped"
		return entry, data, data, nil
	}

	var updated []byte
	if had {
		updated = schemas.SetModeline(data, target)
	} else {
		updated = schemas.EnsureModeline(data, target)
	}
	if bytes.Equal(data, updated) {
		entry.Status = "unchanged"
		return entry, data, data, nil
	}

	var store *trust.Store
	var beforeCheck trust.FileCheck
	wasTrusted := false
	if isProject {
		store = trust.DefaultStore()
		beforeCheck = store.CheckContents(path, data)
		wasTrusted = beforeCheck.Status == trust.Trusted
	}

	if err := fsutil.AtomicWrite(path, updated); err != nil {
		return schemaStampEntry{}, nil, nil, fmt.Errorf("writing %s: %w", path, err)
	}
	if wasTrusted {
		if err := store.Trust(beforeCheck.Path, updated); err != nil {
			return schemaStampEntry{}, nil, nil, fmt.Errorf("stamped %s, but failed to restore trust: %w", path, err)
		}
	}

	if had {
		entry.Status = "updated"
	} else {
		entry.Status = "stamped"
	}
	return entry, data, updated, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
