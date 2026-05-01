package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatRoomCodeCacheSerial evita carreras: direct y relay guardan el ultimo codigo
// en el mismo archivo bajo UserCacheDir (brinco-last-room-code.txt). Sin esto,
// go test puede ejecutar ambos tests en paralelo y el join lee un codigo ajeno.
var chatRoomCodeCacheSerial sync.Mutex

func lockSharedChatRoomCodeCache(t *testing.T) {
	t.Helper()
	chatRoomCodeCacheSerial.Lock()
	t.Cleanup(chatRoomCodeCacheSerial.Unlock)
}

func newIsolatedCacheDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "brinco-e2e-cache-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})
	dir := filepath.Join(root, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withIsolatedCacheEnv(cmd *exec.Cmd, cacheDir string) {
	env := append([]string{}, os.Environ()...)
	originalLocalAppData := os.Getenv("LOCALAPPDATA")
	env = upsertEnv(env, "XDG_CACHE_HOME", cacheDir)
	env = upsertEnv(env, "LOCALAPPDATA", cacheDir)
	env = upsertEnv(env, "APPDATA", cacheDir)
	if goCache := os.Getenv("GOCACHE"); goCache != "" {
		env = upsertEnv(env, "GOCACHE", goCache)
	} else if originalLocalAppData != "" {
		env = upsertEnv(env, "GOCACHE", filepath.Join(originalLocalAppData, "go-build"))
	}
	if goEnv := os.Getenv("GOENV"); goEnv != "" {
		env = upsertEnv(env, "GOENV", goEnv)
	}
	cmd.Env = env
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i := range env {
		if strings.HasPrefix(strings.ToUpper(env[i]), strings.ToUpper(prefix)) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func clearCachedRoomCodeFile(t *testing.T, cacheDir, cacheFile string) {
	t.Helper()
	path := filepath.Join(cacheDir, cacheFile)
	_ = os.Remove(path)
}

func resolveCacheDir(t *testing.T, cacheDir string) string {
	t.Helper()
	if cacheDir != "" {
		return cacheDir
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

const (
	e2eChatLastRoomCodeFile = "brinco-last-room-code.txt"
	e2eP2PLastCodeFile      = "brinco-last-p2p-code.txt"
	e2eTUIReadyMarker       = "Escribe mensajes y Enter para enviar"
)

// waitHostThenReadCachedRoomCode espera a que arranque la TUI y lee el codigo
// guardado en disco (el host ya no imprime el codigo en stdout).
func waitHostThenReadCachedRoomCode(t *testing.T, ctx context.Context, out *safeOutput, cacheDir, cacheFile string) string {
	t.Helper()
	waitForCtx(t, ctx, out, e2eTUIReadyMarker)
	path := filepath.Join(resolveCacheDir(t, cacheDir), cacheFile)
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
