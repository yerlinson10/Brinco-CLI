package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	e2eChatLastRoomCodeFile = "brinco-last-room-code.txt"
	e2eP2PLastCodeFile      = "brinco-last-p2p-code.txt"
	e2eTUIReadyMarker       = "Escribe mensajes y Enter para enviar"
)

// waitHostThenReadCachedRoomCode espera a que arranque la TUI y lee el codigo
// guardado en disco (el host ya no imprime el codigo en stdout).
func waitHostThenReadCachedRoomCode(t *testing.T, ctx context.Context, out *safeOutput, cacheFile string) string {
	t.Helper()
	waitForCtx(t, ctx, out, e2eTUIReadyMarker)
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cacheDir, cacheFile)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatalf("contexto: %v", ctx.Err())
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			if code := extractCode(string(raw)); code != "" {
				return code
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("timeout leyendo codigo en %s", path)
	return ""
}
