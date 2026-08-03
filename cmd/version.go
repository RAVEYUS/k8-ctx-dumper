// Package cmd wires the CLI: the root command with its persistent flags, the
// 'dump' subcommand that performs the actual work, and the 'version' command.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build metadata, injected at build time via:
//
//	go build -ldflags "-X k8s-ctx-dumper/cmd.version=v1.0.0 -X k8s-ctx-dumper/cmd.commit=$(git rev-parse --short HEAD) -X k8s-ctx-dumper/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionCmd prints build information to stdout.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and build information",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("k8s-ctx-dumper %s (commit %s, built %s)\n", version, commit, date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
