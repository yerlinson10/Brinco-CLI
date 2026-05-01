package tui

// LineMsg appends one line to the chat viewport (may contain ANSI).
type LineMsg struct {
	Text string
}

// ClearMsg clears the message viewport (/clear or Ctrl+L).
type ClearMsg struct{}

// HistoryMsg appends multiple lines (e.g. /history).
type HistoryMsg struct {
	Lines []string
}

// StatusMsg updates the header/footer status strip.
type StatusMsg struct {
	Text string
}
