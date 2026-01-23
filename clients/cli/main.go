package main

import (
	"os"

	"github.com/angristan/netclode/clients/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
