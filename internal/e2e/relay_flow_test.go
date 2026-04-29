package e2e

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"testing"
	"time"
)

func TestRelayCreateJoinLeave(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	relayAddr := freeLocalAddr(t)

	relay := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "relay", "serve", "--listen", relayAddr, "--public", relayAddr)
	relay.Dir = repoRoot(t)
	var relayOut bytes.Buffer
	relay.Stdout = &relayOut
	relay.Stderr = &relayOut
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &relayOut, "Relay escuchando", 12*time.Second)

	create := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "relay", "--relay", relayAddr, "--password", "rpass", "--name", "host")
	create.Dir = repoRoot(t)
	var hostOut bytes.Buffer
	create.Stdout = &hostOut
	create.Stderr = &hostOut
	hostIn, _ := create.StdinPipe()
	if err := create.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &hostOut, "Codigo:", 12*time.Second)
	code := extractCode(hostOut.String())
	if code == "" {
		t.Fatalf("sin code relay: %s", hostOut.String())
	}

	join := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "relay", "--code", code, "--password", "rpass", "--name", "guest")
	join.Dir = repoRoot(t)
	var guestOut bytes.Buffer
	join.Stdout = &guestOut
	join.Stderr = &guestOut
	guestIn, _ := join.StdinPipe()
	if err := join.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, &guestOut, "Conectado a la sala relay", 10*time.Second)
	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = join.Wait()
	_ = create.Wait()
	_ = relay.Process.Kill()
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
