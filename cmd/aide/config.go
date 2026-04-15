// cmd/aide/config.go
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/config"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and edit aide configuration",
	}
	cmd.AddCommand(configShowCmd())
	cmd.AddCommand(configEditCmd())
	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Print the config file contents",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath := config.FilePath()
			data, err := os.ReadFile(configPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "No config file found. Run `aide init` to create one.")
					return nil
				}
				return fmt.Errorf("reading config file: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "# %s\n", configPath)
			fmt.Fprint(out, string(data))
			return nil
		},
	}
}

func configEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit",
		Short:        "Open the config file in your editor",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath := config.FilePath()
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				return fmt.Errorf("no config file found. Run `aide init` to create one")
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = os.Getenv("VISUAL")
			}
			if editor == "" {
				editor = "vi"
			}

			editorCmd := exec.Command(editor, configPath)
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr
			if err := editorCmd.Run(); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}

			out := cmd.OutOrStdout()
			if _, err := cmdEnv(cmd); err != nil {
				fmt.Fprintf(out, "Saved. Validation failed: %s\n", err)
			} else {
				fmt.Fprintln(out, "Saved. Validation: OK")
			}
			return nil
		},
	}
}
