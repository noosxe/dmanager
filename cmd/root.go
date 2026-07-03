package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dmanager",
	Short: "dmanager is a Docker Container Manager web application",
	Long:  `A self-contained Docker Container Manager web application that discovers local containers, allows start/stop operations, and conducts scheduled image update checks.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("dmanager CLI. Use --help to view available commands.")
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
