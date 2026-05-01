package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
)

// Options configures the chat TUI header.
type Options struct {
	Title string
	Mode  string
	Nick  string
}

// App drives Bubble Tea or legacy line mode for interactive chat.
type App struct {
	mu        sync.Mutex
	useTea    bool
	prog      *tea.Program
	lines     chan string
	closed    atomic.Bool
	closeOnce sync.Once
	opts      Options
	interrupt atomic.Int32
}

// New creates an App. Call Start before Push / receiving from Lines.
func New(opts Options) *App {
	return &App{
		useTea: InteractiveTUIEnabled(),
		lines:  make(chan string, 512),
		opts:   opts,
	}
}

// Start launches the TUI program or stdin scanner (legacy).
func (a *App) Start() error {
	if !a.useTea {
		go a.scanStdin()
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.prog != nil {
		return nil
	}
	m := newChatModel(a.opts, a.lines, a)
	// Sin WithMouseCellMotion: el terminal puede usar el raton para seleccionar/copiar texto del scroll.
	a.prog = tea.NewProgram(m,
		tea.WithAltScreen(),
	)
	go func() {
		_, _ = a.prog.Run()
		a.closeLinesOnce()
	}()
	return nil
}

func (a *App) scanStdin() {
	defer a.closeLinesOnce()
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		if a.closed.Load() {
			return
		}
		a.lines <- strings.TrimSpace(sc.Text())
	}
}

func (a *App) closeLinesOnce() {
	a.closeOnce.Do(func() {
		close(a.lines)
	})
}

// Lines yields trimmed lines from the user (Enter in TUI or legacy stdin).
func (a *App) Lines() <-chan string {
	return a.lines
}

// PushLine appends one line to the scrollback (ANSI allowed).
func (a *App) PushLine(text string) {
	a.Push(LineMsg{Text: text})
}

// PushClear clears the message viewport.
func (a *App) PushClear() {
	a.Push(ClearMsg{})
}

// PushHistory appends multiple lines (e.g. /history).
func (a *App) PushHistory(lines []string) {
	a.Push(HistoryMsg{Lines: lines})
}

// SetStatus updates the header status strip.
func (a *App) SetStatus(text string) {
	a.Push(StatusMsg{Text: text})
}

// Push sends a custom message to the TUI (LineMsg, ClearMsg, HistoryMsg, StatusMsg).
func (a *App) Push(msg tea.Msg) {
	if a.closed.Load() {
		return
	}
	if !a.useTea {
		switch m := msg.(type) {
		case LineMsg:
			fmt.Println(m.Text)
		case ClearMsg:
			fmt.Print("\033[2J\033[H")
		case HistoryMsg:
			for _, ln := range m.Lines {
				fmt.Println(ln)
			}
		case StatusMsg:
			// no-op in legacy (status only in full TUI)
		}
		return
	}
	a.mu.Lock()
	p := a.prog
	a.mu.Unlock()
	if p != nil {
		p.Send(msg)
	}
}

// Shutdown stops the program and closes the Lines channel.
func (a *App) Shutdown() {
	if a.closed.Swap(true) {
		return
	}
	a.mu.Lock()
	p := a.prog
	a.mu.Unlock()
	if p != nil {
		p.Quit()
	}
	a.closeLinesOnce()
}

// UsedTTY is true when Bubble Tea (alt screen) is active.
func (a *App) UsedTTY() bool {
	return a.useTea
}

// InterruptRequested is true after Ctrl+C in the TUI.
func (a *App) InterruptRequested() bool {
	return a.interrupt.Load() != 0
}

// ResetInterrupt clears the interrupt flag.
func (a *App) ResetInterrupt() {
	a.interrupt.Store(0)
}
