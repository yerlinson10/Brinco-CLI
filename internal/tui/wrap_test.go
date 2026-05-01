package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapLogTextLongUnbroken(t *testing.T) {
	in := strings.Repeat("A", 50)
	out := wrapLogText(in, 12)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d: %q", len(lines), out)
	}
	for _, ln := range lines {
		if w := ansi.StringWidth(ln); w > 12 {
			t.Fatalf("line too wide: %d > 12: %q", w, ln)
		}
	}
}

func TestWrapLogTextPreservesParagraphBreak(t *testing.T) {
	out := wrapLogText("first line\n\nsecond", 20)
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected blank line between paragraphs: %q", out)
	}
}
