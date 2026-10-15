package main

import (
	"os"

	// tzdata ships the IANA time zone database inside the binary (ADR 0009),
	// so time.LoadLocation works on machines with no TZ database.
	_ "time/tzdata"

	"github.com/aries1980/lodo/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
