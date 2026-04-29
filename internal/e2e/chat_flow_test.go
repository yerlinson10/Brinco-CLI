package e2e

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDirectCreateJoinSendLeave(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	createCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "direct", "--listen", "127.0.0.1:19091", "--public", "127.0.0.1:19091", "--password", "p4ss", "--name", "host")
	createCmd.Dir = repoRoot(t)
	var hostOut bytes.Buffer
	createCmd.Stdout = &hostOut
	createCmd.Stderr = &hostOut
	hostIn, err := createCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := createCmd.Start(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, &hostOut, "Codigo de sala:", 12*time.Second)
	code := extractCode(hostOut.String())
	if code == "" {
		t.Fatalf("no se pudo extraer code: %s", hostOut.String())
	}

	joinCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "direct", "--code", code, "--password", "p4ss", "--name", "guest")
	joinCmd.Dir = repoRoot(t)
	var guestOut bytes.Buffer
	joinCmd.Stdout = &guestOut
	joinCmd.Stderr = &guestOut
	guestIn, err := joinCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := joinCmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &guestOut, "Conectado a la sala", 10*time.Second)
	if _, err := guestIn.Write([]byte("hola-e2e\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &hostOut, "hola-e2e", 10*time.Second)
	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = joinCmd.Wait()
	_ = createCmd.Wait()
}

func waitFor(t *testing.T, b *bytes.Buffer, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), needle) {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
	t.Fatalf("timeout esperando %q en salida: %s", needle, b.String())
}

func extractCode(out string) string {
	re := regexp.MustCompile(`(?:p2p|direct|relay|guaranteed)-[A-Za-z0-9_-]+`)
	if m := re.FindString(out); m != "" {
		return m
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "Codigo de sala:") {
			parts := strings.SplitN(line, "Codigo de sala:", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
		if strings.Contains(line, "Codigo:") {
			parts := strings.SplitN(line, "Codigo:", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no se pudo detectar ruta del test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
