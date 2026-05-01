package e2e

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestP2PCreateJoinEncryptedChatAndSystem comprueba el flujo libp2p: mensajes de chat
// van cifrados (nonce/cipher) y deben descifrarse y mostrarse en el otro peer; los
// system siguen en JSON en claro y deben mostrarse sin pasar por AES-GCM.
func TestP2PCreateJoinEncryptedChatAndSystem(t *testing.T) {
	cacheDir := newIsolatedCacheDir(t)
	clearCachedRoomCodeFile(t, cacheDir, e2eP2PLastCodeFile)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	createCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "create", "--mode", "p2p", "--name", "host")
	createCmd.Dir = repoRoot(t)
	withIsolatedCacheEnv(createCmd, cacheDir)
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

	code := waitHostThenReadCachedRoomCode(t, ctx, &hostOut, cacheDir, e2eP2PLastCodeFile)
	if code == "" {
		t.Fatalf("no se pudo extraer code p2p: %s", hostOut.String())
	}

	joinCmd := exec.CommandContext(ctx, "go", "run", "./cmd/brinco", "room", "join", "--mode", "p2p", "--code", code, "--name", "guest")
	joinCmd.Dir = repoRoot(t)
	withIsolatedCacheEnv(joinCmd, cacheDir)
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

	waitForCtx(t, ctx, &guestOut, "Conectado al topic de la sala")

	// Mensaje system en claro al unirse (Publish type system, sin cifrar).
	waitForCtx(t, ctx, &hostOut, "se unio")

	const chatToken = "e2e-p2p-cifrado-xyz"
	if _, err := guestIn.Write([]byte(chatToken + "\n")); err != nil {
		t.Fatal(err)
	}
	// Chat cifrado: el host debe mostrar el texto tras descifrar.
	waitForCtx(t, ctx, &hostOut, chatToken)

	_, _ = guestIn.Write([]byte("/quit\n"))
	_, _ = hostIn.Write([]byte("/quit\n"))
	_ = joinCmd.Wait()
	_ = createCmd.Wait()
}
