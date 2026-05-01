package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chatModel struct {
	opts    Options
	linesCh chan<- string
	app     *App

	vp       viewport.Model
	ti       textinput.Model
	scroll   strings.Builder
	status   string
	width    int
	height   int
	inputBuf []string
	histIdx int
}

func newChatModel(opts Options, linesCh chan<- string, app *App) *chatModel {
	ti := textinput.New()
	ti.Placeholder = "Mensaje o /help …"
	ti.Focus()
	ti.EchoMode = textinput.EchoNormal
	ti.CharLimit = 8192
	ti.Width = 40

	vp := viewport.New(72, 18)
	vp.MouseWheelEnabled = true

	return &chatModel{
		opts:     opts,
		linesCh:  linesCh,
		app:      app,
		vp:       vp,
		ti:       ti,
		inputBuf: []string{},
		histIdx:  -1,
	}
}

func (m *chatModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *chatModel) syncViewport() {
	m.vp.SetContent(m.scroll.String())
	m.vp.GotoBottom()
}

func (m *chatModel) wrapWidth() int {
	inner := m.vp.Width - m.vp.Style.GetHorizontalFrameSize()
	if inner >= minWrapWidth {
		return inner
	}
	if m.width >= minWrapWidth+2 {
		return m.width - 2
	}
	return minWrapWidth
}

func (m *chatModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	const headerRows = 2
	const promptRows = 1
	vpH := m.height - headerRows - promptRows
	if vpH < 4 {
		vpH = 4
	}
	m.vp.Width = m.width
	m.vp.Height = vpH
	m.ti.Width = max(10, m.width-4)
}

func (m *chatModel) renderHeader() string {
	title := titleStyle.Render(strings.TrimSpace(m.opts.Title))
	sub := subStyle.Render(fmt.Sprintf("%s · %s", strings.TrimSpace(m.opts.Mode), strings.TrimSpace(m.opts.Nick)))
	line1 := lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", sub)
	st := strings.TrimSpace(m.status)
	var line2 string
	if st != "" {
		line2 = statusStyle.Render(st)
	}
	return lipgloss.JoinVertical(lipgloss.Left, line1, line2)
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.syncViewport()
		return m, nil

	case LineMsg:
		if m.scroll.Len() > 0 {
			m.scroll.WriteByte('\n')
		}
		m.scroll.WriteString(wrapLogText(msg.Text, m.wrapWidth()))
		m.syncViewport()
		return m, nil

	case ClearMsg:
		m.scroll.Reset()
		m.vp.SetContent("")
		return m, nil

	case HistoryMsg:
		if len(msg.Lines) == 0 {
			return m, nil
		}
		if m.scroll.Len() > 0 {
			m.scroll.WriteByte('\n')
		}
		m.scroll.WriteString(wrapLogText(strings.Join(msg.Lines, "\n"), m.wrapWidth()))
		m.syncViewport()
		return m, nil

	case StatusMsg:
		m.status = msg.Text
		return m, nil

	case tea.MouseMsg:
		v, cmd := m.vp.Update(msg)
		m.vp = v
		return m, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.app != nil {
				m.app.interrupt.Store(1)
			}
			return m, tea.Quit
		case "ctrl+l":
			m.scroll.Reset()
			m.vp.SetContent("")
			return m, nil
		case "pgup":
			m.vp.ViewUp()
			return m, nil
		case "pgdown":
			m.vp.ViewDown()
			return m, nil
		case "up":
			if len(m.inputBuf) == 0 {
				break
			}
			if m.histIdx < 0 {
				m.histIdx = len(m.inputBuf) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.ti.SetValue(m.inputBuf[m.histIdx])
			m.ti.CursorEnd()
			return m, nil
		case "down":
			if m.histIdx < 0 {
				break
			}
			if m.histIdx >= len(m.inputBuf)-1 {
				m.histIdx = -1
				m.ti.SetValue("")
			} else {
				m.histIdx++
				m.ti.SetValue(m.inputBuf[m.histIdx])
				m.ti.CursorEnd()
			}
			return m, nil
		case "enter":
			line := strings.TrimSpace(m.ti.Value())
			if line == "" {
				return m, nil
			}
			select {
			case m.linesCh <- line:
			default:
			}
			m.inputBuf = append(m.inputBuf, line)
			if len(m.inputBuf) > 200 {
				m.inputBuf = m.inputBuf[len(m.inputBuf)-200:]
			}
			m.histIdx = -1
			m.ti.SetValue("")
			m.ti.CursorEnd()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m *chatModel) View() string {
	if m.width <= 0 {
		return "…"
	}
	header := m.renderHeader()
	vp := m.vp.View()
	prompt := promptStyle.Render("> ") + m.ti.View()
	return lipgloss.JoinVertical(lipgloss.Left, header, vp, prompt)
}
