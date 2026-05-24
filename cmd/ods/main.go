package main

import (
	"os"

	"github.com/open-delivery-spec/cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
