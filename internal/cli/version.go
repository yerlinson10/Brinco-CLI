package cli

import "strings"

// buildVersion is the embedded default; override from main with -ldflags
// -X main.version=... and cli.SetBuildVersion from main().
var buildVersion = "2.0.0"

// SetBuildVersion sets the CLI version string (e.g. from main.version at link time).
// Empty or whitespace-only values are ignored so the default stays in tests/tools.
func SetBuildVersion(v string) {
	if strings.TrimSpace(v) != "" {
		buildVersion = strings.TrimSpace(v)
	}
}

// Version returns the active CLI version for help, doctor, and updater.
func Version() string {
	return buildVersion
}
