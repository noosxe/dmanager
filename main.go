// Package main is the entrypoint for the dmanager application.
package main

import (
	"embed"

	"dmanager/cmd"
)

//go:embed all:frontend/dist
var frontendDist embed.FS

func main() {
	cmd.FrontendDist = frontendDist
	cmd.Execute()
}
