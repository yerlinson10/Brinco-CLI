package e2e

import (
	"bufio"
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
	lockSharedChatRoomCodeCache(t)
	clearSharedChatRoomCodeCacheFile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	createCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "direct", "--listen", "127.0.0.1:19091", "--public", "127.0.0.1:19091", "--password", "p4ss", "--name", "host")
	createCmd.Dir = repoRoot(t)
	var hostOut safeOutput
	createCmd.Stdout = &hostOut
	createCmd.Stderr = &hostOut
	hostIn, err := createCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := createCmd.Start(); err != nil {
		t.Fatal(err)
	}

	code := waitHostThenReadCachedRoomCode(t, ctx, &hostOut, e2eChatLastRoomCodeFile)
	if code == "" {
		t.Fatalf("no se pudo extraer code: %s", hostOut.String())
	}

	joinCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "direct", "--code", code, "--password", "p4ss", "--name", "guest")
	joinCmd.Dir = repoRoot(t)
	var guestOut safeOutput
	joinCmd.Stdout = &guestOut
	joinCmd.Stderr = &guestOut
	guestIn, err := joinCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := joinCmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForCtx(t, ctx, &guestOut, "Conectado a la sala")
	if _, err := guestIn.Write([]byte("hola-e2e\n")); err != nil {
		t.Fatal(err)
	}
	waitForCtx(t, ctx, &hostOut, "hola-e2e")
	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = joinCmd.Wait()
	_ = createCmd.Wait()
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
