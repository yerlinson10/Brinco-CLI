package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const copyTimeout = 2 * time.Second

// Copy escribe s en el portapapeles del sistema (best-effort).
// Windows: clip. macOS: pbcopy. Linux: wl-copy, xclip o xsel si existen.
func Copy(s string) error {
	if s == "" {
		return nil
	}
	ctx := context.Background()
	switch runtime.GOOS {
	case "windows":
		runCtx, cancel := context.WithTimeout(ctx, copyTimeout)
		defer cancel()
		cmd := exec.CommandContext(runCtx, "cmd", "/c", "clip")
		cmd.Stdin = strings.NewReader(s)
		return cmd.Run()
	case "darwin":
		runCtx, cancel := context.WithTimeout(ctx, copyTimeout)
		defer cancel()
		cmd := exec.CommandContext(runCtx, "pbcopy")
		cmd.Stdin = strings.NewReader(s)
		return cmd.Run()
	default:
		return copyLinux(ctx, s)
	}
}

func copyLinux(ctx context.Context, s string) error {
	attempts := []struct {
		name string
		args []string
	}{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
	}
	var errs []error
	for _, a := range attempts {
		err := copyStdin(ctx, a.name, s, a.args...)
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", a.name, err))
	}
	return fmt.Errorf(
		"portapapeles en Linux: ninguno de wl-copy, xclip ni xsel funcionó: %w",
		errors.Join(errs...),
	)
}

func copyStdin(ctx context.Context, name, stdin string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("no está en PATH: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// RoomCodeFeedbackLines muestra el codigo en el chat e intenta copiarlo al portapapeles.
// Devuelve lineas listas para imprimir o enviar a la TUI (orden: codigo, resultado o aviso).
func RoomCodeFeedbackLines(code string) []string {
	c := strings.TrimSpace(code)
	if c == "" {
		return []string{"No hay codigo disponible en esta sesion"}
	}
	out := []string{"Codigo de sala: " + c}
	if err := Copy(c); err != nil {
		out = append(out, fmt.Sprintf("Aviso: no se pudo copiar al portapapeles: %v", err))
		return out
	}
	out = append(out, "Codigo copiado al portapapeles.")
	return out
}
