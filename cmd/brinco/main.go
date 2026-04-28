package main

import (
	"os"

	"brinco-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
