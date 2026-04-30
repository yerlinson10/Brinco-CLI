package e2e

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

const waitForOutputPollInterval = 120 * time.Millisecond

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

func (s *safeOutput) Contains(needle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.b.String(), needle)
}

// waitForCtx espera hasta que out contenga needle o hasta que ctx expire.
// Comparte el mismo deadline que exec.CommandContext para evitar flakes en CI.
func waitForCtx(t *testing.T, ctx context.Context, out *safeOutput, needle string) {
	t.Helper()
	for {
		snapshot := out.String()
		if ctx.Err() != nil {
			t.Fatalf("timeout esperando %q en salida (contexto): %v\n%s", needle, ctx.Err(), snapshot)
		}
		if strings.Contains(snapshot, needle) {
			return
		}
		time.Sleep(waitForOutputPollInterval)
	}
}
