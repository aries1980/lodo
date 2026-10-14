package main

import (
	"os"

	"github.com/aries1980/lodo/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
