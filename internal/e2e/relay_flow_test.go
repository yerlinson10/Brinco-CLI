package e2e

import (
	"context"
	"net"
	"os/exec"
	"testing"
	"time"
)

func TestRelayCreateJoinLeave(t *testing.T) {
	lockSharedChatRoomCodeCache(t)
	cacheDir := newIsolatedCacheDir(t)
	clearCachedRoomCodeFile(t, cacheDir, e2eChatLastRoomCodeFile)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	relayAddr := freeLocalAddr(t)

	relay := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "relay", "serve", "--listen", relayAddr, "--public", relayAddr)
	relay.Dir = repoRoot(t)
	withIsolatedCacheEnv(relay, cacheDir)
	var relayOut safeOutput
	relay.Stdout = &relayOut
	relay.Stderr = &relayOut
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if relay.Process != nil {
			_ = relay.Process.Kill()
		}
	})
	waitForCtx(t, ctx, &relayOut, "Relay escuchando")

	create := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "relay", "--relay", relayAddr, "--password", "rpass", "--name", "host")
	create.Dir = repoRoot(t)
	withIsolatedCacheEnv(create, cacheDir)
	var hostOut safeOutput
	create.Stdout = &hostOut
	create.Stderr = &hostOut
	hostIn, _ := create.StdinPipe()
	if err := create.Start(); err != nil {
		t.Fatal(err)
	}
	code := waitHostThenReadCachedRoomCode(t, ctx, &hostOut, cacheDir, e2eChatLastRoomCodeFile)
	if code == "" {
		t.Fatalf("sin code relay: %s", hostOut.String())
	}

	join := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "relay", "--code", code, "--password", "rpass", "--name", "guest")
	join.Dir = repoRoot(t)
	withIsolatedCacheEnv(join, cacheDir)
	var guestOut safeOutput
	join.Stdout = &guestOut
	join.Stderr = &guestOut
	guestIn, _ := join.StdinPipe()
	if err := join.Start(); err != nil {
		t.Fatal(err)
	}
	waitForCtx(t, ctx, &guestOut, "Conectado a la sala relay")
	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = join.Wait()
	_ = create.Wait()
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
