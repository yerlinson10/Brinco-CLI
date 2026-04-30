package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"brinco-cli/internal/roomproto"
)

// stdin is a single buffered reader for os.Stdin so sequential prompts do not
// attach multiple bufio.Reader instances to the same file descriptor.
var stdin = bufio.NewReader(os.Stdin)

func readLineTrim(prompt string) (string, error) {
	if _, err := fmt.Fprint(os.Stderr, prompt); err != nil {
		return "", err
	}
	line, err := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil {
		if err == io.EOF {
			if line == "" {
				return "", io.EOF
			}
			return line, nil
		}
		return "", err
	}
	return line, nil
}

func readLineTrimOrDefault(prompt, defaultVal string) (string, error) {
	s, err := readLineTrim(prompt)
	if err != nil {
		return "", err
	}
	if s == "" {
		return defaultVal, nil
	}
	return s, nil
}

func readOptionalPasswordLine() (string, error) {
	fmt.Fprintln(os.Stderr, "Password de la sala (Enter = sin contraseña, la sala queda abierta):")
	s, err := readLineTrim("")
	if err != nil {
		return "", err
	}
	return s, nil
}

func looksLikeRoomCode(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "@") {
		return false
	}
	p, _ := roomproto.Unwrap(s)
	return p != ""
}
