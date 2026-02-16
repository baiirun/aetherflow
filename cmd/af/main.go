package main

import (
	"os"

	"github.com/baiirun/aetherflow/cmd/af/cmd"
)

// version is set by goreleaser via ldflags at build time.
var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
