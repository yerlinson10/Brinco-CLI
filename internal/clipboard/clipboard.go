package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Copy escribe s en el portapapeles del sistema (best-effort).
// Windows: clip. macOS: pbcopy. Linux: wl-copy, xclip o xsel si existen.
func Copy(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "clip")
		cmd.Stdin = strings.NewReader(s)
		return cmd.Run()
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(s)
		return cmd.Run()
	default:
		if err := copyStdin("wl-copy", s); err == nil {
			return nil
		}
		if err := copyStdin("xclip", s, "-selection", "clipboard"); err == nil {
			return nil
		}
		return copyStdin("xsel", s, "--clipboard", "--input")
	}
}

func copyStdin(name, stdin string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// AnnounceRoomCode copia code al portapapeles e informa en consola.
func AnnounceRoomCode(code string) {
	if err := Copy(code); err != nil {
		fmt.Fprintf(os.Stderr, "Aviso: no se pudo copiar el codigo al portapapeles: %v\n", err)
		return
	}
	fmt.Println("Codigo copiado al portapapeles.")
}
