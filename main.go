package main

import (
	"os"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

func main() {
	// See cmd/root.go for Execute().
	if err := cmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
