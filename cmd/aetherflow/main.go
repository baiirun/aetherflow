package main

import (
	"os"

	"github.com/geobrowser/aetherflow/cmd/aetherflow/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
