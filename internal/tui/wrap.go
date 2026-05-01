package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const minWrapWidth = 8

// wrapLogText folds text to maxWidth terminal cells per line (ANSI-aware).
// Preserves explicit newlines in s as paragraph breaks; long tokens are hard-wrapped.
func wrapLogText(s string, maxWidth int) string {
	if maxWidth < minWrapWidth {
		maxWidth = minWrapWidth
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	parts := strings.Split(s, "\n")
	var b strings.Builder
	for i, line := range parts {
		if i > 0 {
			b.WriteByte('\n')
		}
		if line == "" {
			continue
		}
		// Wrap breaks at spaces when possible; long tokens (URLs, codes) are hard-wrapped to maxWidth.
		b.WriteString(ansi.Wrap(line, maxWidth, " \t/-_"))
	}
	return b.String()
}
