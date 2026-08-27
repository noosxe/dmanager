// Package cmd contains the CLI command setup and execution handlers.
package cmd

import (
	"embed"
	"fmt"
	"os"

	"dmanager/internal/version"

	"github.com/spf13/cobra"
)

// FrontendDist holds the embedded frontend static assets.
var FrontendDist embed.FS

var rootCmd = &cobra.Command{
	Use:   "dmanager",
	Short: "dmanager is a Docker Container Manager web application",
	Long:  `A self-contained Docker Container Manager web application that discovers local containers, allows start/stop operations, and conducts scheduled image update checks.`,
	// Setting Version makes cobra register the --version flag.
	Version: version.String(),
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("dmanager CLI. Use --help to view available commands.")
	},
}

// Execute runs the root command and handles command-line routing.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
