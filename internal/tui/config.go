package tui

import (
	"os"
	"strings"
	"sync/atomic"

	"github.com/mattn/go-isatty"
)

// forceLegacy disables Bubble Tea when true (--no-tui or BRINCO_TUI=0).
var forceLegacy atomic.Bool

// SetForceLegacy forces line-mode stdin/stdout (no raw TUI).
func SetForceLegacy(v bool) {
	forceLegacy.Store(v)
}

// InteractiveTUIEnabled reports whether the full-screen chat TUI should run.
func InteractiveTUIEnabled() bool {
	if forceLegacy.Load() {
		return false
	}
	if strings.TrimSpace(os.Getenv("BRINCO_TUI")) == "0" {
		return false
	}
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}
