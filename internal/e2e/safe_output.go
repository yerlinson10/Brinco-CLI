package e2e

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeOutput acumula stdout/stderr de un exec.Cmd de forma segura entre
// goroutines (el proceso hijo escribe mientras el test lee en waitForCtx).
type safeOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeOutput) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeOutput) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitForCtx espera hasta que out contenga needle o hasta que ctx expire.
// Comparte el mismo deadline que exec.CommandContext para evitar flakes en CI.
func waitForCtx(t *testing.T, ctx context.Context, out *safeOutput, needle string) {
	t.Helper()
	for {
		if ctx.Err() != nil {
			t.Fatalf("timeout esperando %q en salida (contexto): %v\n%s", needle, ctx.Err(), out.String())
		}
		if strings.Contains(out.String(), needle) {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
}
