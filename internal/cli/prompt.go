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

func looksLikeRoomCode(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "@") {
		return false
	}
	p, _ := roomproto.Unwrap(s)
	return p != ""
}
