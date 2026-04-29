package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"brinco-cli/internal/roomproto"
)

func readLineTrim(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func readPasswordLine(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s (visible al escribir): ", strings.TrimSpace(prompt))
	return readLineTrim("")
}

func readLineTrimOrDefault(prompt, defaultVal string) (string, error) {
	s, err := readLineTrim(prompt)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return defaultVal, nil
	}
	return strings.TrimSpace(s), nil
}

func readOptionalPasswordLine() (string, error) {
	fmt.Fprintln(os.Stderr, "Password de la sala (Enter = sin contraseña, la sala queda abierta):")
	s, err := readLineTrim("")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func looksLikeRoomCode(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "@") {
		return false
	}
	p, _ := roomproto.Unwrap(s)
	return p != ""
}
