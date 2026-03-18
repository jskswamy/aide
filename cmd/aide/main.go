package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "aide",
		Short:   "Universal Coding Agent Context Manager",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("aide %s\n", version)
			return nil
		},
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
