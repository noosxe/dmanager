// Package cmd contains the CLI command setup and execution handlers.
package cmd

import (
	"embed"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Build metadata, injected at link time via
// -ldflags "-X dmanager/cmd.version=<ver> -X dmanager/cmd.commit=<sha> -X dmanager/cmd.date=<isotime>".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// FrontendDist holds the embedded frontend static assets.
var FrontendDist embed.FS

var rootCmd = &cobra.Command{
	Use:   "dmanager",
	Short: "dmanager is a Docker Container Manager web application",
	Long:  `A self-contained Docker Container Manager web application that discovers local containers, allows start/stop operations, and conducts scheduled image update checks.`,
	// Setting Version makes cobra register the --version flag.
	Version: versionString(),
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("dmanager CLI. Use --help to view available commands.")
	},
}

// versionString renders the value reported by --version. Without linker
// injection (plain "go build") it degrades to the bare version string.
func versionString() string {
	if commit == "none" {
		return version
	}
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// Execute runs the root command and handles command-line routing.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
