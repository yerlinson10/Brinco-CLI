package main

import (
	"os"
	"strings"

	"brinco-cli/internal/cli"
)

// Optional override at link time: -ldflags "-X main.version=1.2.2"
// If empty, internal/cli uses its embedded default.
var version string

func main() {
	if s := strings.TrimSpace(version); s != "" {
		cli.SetBuildVersion(s)
	}
	os.Exit(cli.Run(os.Args[1:]))
}
