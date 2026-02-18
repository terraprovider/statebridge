package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Commit = "none"
var Tag = "v0.0.0"
var BuildDate = "unknown"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("tfmigrate %s (commit: %s, built: %s)\n", Tag, Commit, BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
