package e2e

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestP2PCreateJoinEncryptedChatAndSystem comprueba el flujo libp2p: mensajes de chat
// van cifrados (nonce/cipher) y deben descifrarse y mostrarse en el otro peer; los
// system siguen en JSON en claro y deben mostrarse sin pasar por AES-GCM.
func TestP2PCreateJoinEncryptedChatAndSystem(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	createCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "p2p", "--name", "host")
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

	waitFor(t, &hostOut, "Codigo de sala:", 50*time.Second)
	code := extractCode(hostOut.String())
	if code == "" {
		t.Fatalf("no se pudo extraer code p2p: %s", hostOut.String())
	}

	joinCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "p2p", "--code", code, "--name", "guest")
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

	waitFor(t, &guestOut, "Conectado al topic de la sala", 50*time.Second)

	// Mensaje system en claro al unirse (Publish type system, sin cifrar).
	waitFor(t, &hostOut, "se unio", 45*time.Second)

	const chatToken = "e2e-p2p-cifrado-xyz"
	if _, err := guestIn.Write([]byte(chatToken + "\n")); err != nil {
		t.Fatal(err)
	}
	// Chat cifrado: el host debe mostrar el texto tras descifrar.
	waitFor(t, &hostOut, chatToken, 45*time.Second)

	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = joinCmd.Wait()
	_ = createCmd.Wait()
}
