package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChatModelWindowLineClear(t *testing.T) {
	ch := make(chan string, 8)
	a := &App{useTea: true, lines: ch}
	m := newChatModel(Options{Title: "Test", Mode: "relay", Nick: "u"}, ch, a)

	var mod tea.Model = m
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	cm := mod.(*chatModel)

	mod, _ = cm.Update(LineMsg{Text: "hello"})
	cm = mod.(*chatModel)
	if cm.scroll.Len() == 0 {
		t.Fatal("expected scroll content after LineMsg")
	}

	mod, _ = cm.Update(ClearMsg{})
	cm = mod.(*chatModel)
	if cm.scroll.Len() != 0 {
		t.Fatalf("expected empty scroll after ClearMsg, got %q", cm.scroll.String())
	}
}

func TestChatModelEnterSendsLine(t *testing.T) {
	ch := make(chan string, 8)
	a := &App{useTea: true, lines: ch}
	m := newChatModel(Options{Title: "T", Mode: "m", Nick: "n"}, ch, a)

	var mod tea.Model = m
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	cm := mod.(*chatModel)

	cm.ti.SetValue("  /peers  ")
	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = mod.(*chatModel)

	select {
	case got := <-ch:
		if got != "/peers" {
			t.Fatalf("expected trimmed /peers, got %q", got)
		}
	default:
		t.Fatal("expected line on channel")
	}
	if cm.ti.Value() != "" {
		t.Fatal("expected input cleared after Enter")
	}
}

func TestChatModelHistoryMsg(t *testing.T) {
	ch := make(chan string, 2)
	a := &App{useTea: true, lines: ch}
	m := newChatModel(Options{Title: "T", Mode: "m", Nick: "n"}, ch, a)

	var mod tea.Model = m
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	cm := mod.(*chatModel)

	mod, _ = cm.Update(HistoryMsg{Lines: []string{"1) a", "2) b"}})
	cm = mod.(*chatModel)
	if cm.scroll.Len() == 0 {
		t.Fatal("expected history in scroll")
	}
}

func TestChatModelInputHistoryUpDown(t *testing.T) {
	ch := make(chan string, 8)
	a := &App{useTea: true, lines: ch}
	m := newChatModel(Options{Title: "T", Mode: "m", Nick: "n"}, ch, a)

	var mod tea.Model = m
	mod, _ = mod.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	cm := mod.(*chatModel)

	cm.ti.SetValue("line-a")
	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = mod.(*chatModel)
	drainOne(t, ch, "line-a")

	cm.ti.SetValue("line-b")
	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = mod.(*chatModel)
	drainOne(t, ch, "line-b")

	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	cm = mod.(*chatModel)
	if cm.ti.Value() != "line-b" {
		t.Fatalf("first Up want line-b, got %q", cm.ti.Value())
	}
	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	cm = mod.(*chatModel)
	if cm.ti.Value() != "line-a" {
		t.Fatalf("second Up want line-a, got %q", cm.ti.Value())
	}
	mod, _ = cm.Update(tea.KeyMsg{Type: tea.KeyDown})
	cm = mod.(*chatModel)
	if cm.ti.Value() != "line-b" {
		t.Fatalf("Down want line-b, got %q", cm.ti.Value())
	}
}

func drainOne(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("channel: want %q, got %q", want, got)
		}
	default:
		t.Fatal("expected one line on channel")
	}
}
