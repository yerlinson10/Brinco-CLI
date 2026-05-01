package p2p

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadLastCode(t *testing.T) {
	setCacheEnv(t, t.TempDir())

	want := "p2p-abc123"
	if err := saveLastCode("  " + want + "  "); err != nil {
		t.Fatalf("saveLastCode() error = %v", err)
	}
	got, err := loadLastCode()
	if err != nil {
		t.Fatalf("loadLastCode() error = %v", err)
	}
	if got != want {
		t.Fatalf("loadLastCode() = %q, want %q", got, want)
	}
}

func TestLoadLastCodeMissingFile(t *testing.T) {
	setCacheEnv(t, t.TempDir())

	_, err := loadLastCode()
	if !errors.Is(err, ErrNoSavedCode) {
		t.Fatalf("loadLastCode() err = %v, want ErrNoSavedCode", err)
	}
}

func TestSaveLastCodeRejectsEmpty(t *testing.T) {
	if err := saveLastCode("   "); !errors.Is(err, ErrNoSavedCode) {
		t.Fatalf("saveLastCode() err = %v, want ErrNoSavedCode", err)
	}
}

func TestLoadLastCodeRejectsEmptyFile(t *testing.T) {
	cache := t.TempDir()
	setCacheEnv(t, cache)

	path := filepath.Join(cache, lastCodeFile)
	if err := os.WriteFile(path, []byte(" \n\t "), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	_, err := loadLastCode()
	if !errors.Is(err, ErrNoSavedCode) {
		t.Fatalf("loadLastCode() err = %v, want ErrNoSavedCode", err)
	}
}

func setCacheEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
}

func TestSaveAndLoadLastWorkingPeer(t *testing.T) {
	setCacheEnv(t, t.TempDir())
	topic := "brinco-roomtest"
	addr := "/ip4/1.2.3.4/tcp/4001/p2p/QmAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := saveLastWorkingPeer(topic, addr); err != nil {
		t.Fatalf("saveLastWorkingPeer: %v", err)
	}
	got, ok := loadLastWorkingPeer(topic)
	if !ok || got != addr {
		t.Fatalf("loadLastWorkingPeer = %q ok=%v want %q", got, ok, addr)
	}
}

func TestOrderPeersPreferStored(t *testing.T) {
	setCacheEnv(t, t.TempDir())
	topic := "brinco-xyz"
	stored := "/dns4/relay.example/tcp/443/p2p/QmBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	codePeers := []string{
		"/ip4/10.0.0.1/tcp/4001/p2p/QmCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
	}
	if err := saveLastWorkingPeer(topic, stored); err != nil {
		t.Fatal(err)
	}
	out, used := orderPeersPreferStored(topic, codePeers)
	if !used {
		t.Fatal("expected usedStored true")
	}
	if len(out) != 2 || out[0] != stored {
		t.Fatalf("got %#v", out)
	}
	if out[1] != codePeers[0] {
		t.Fatalf("second peer: %q", out[1])
	}
}

func TestOrderPeersPreferStoredDedup(t *testing.T) {
	setCacheEnv(t, t.TempDir())
	topic := "brinco-dedup"
	same := "/ip4/9.9.9.9/tcp/1/p2p/QmDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
	if err := saveLastWorkingPeer(topic, "  "+same+"  "); err != nil {
		t.Fatal(err)
	}
	out, _ := orderPeersPreferStored(topic, []string{same, "/other"})
	if len(out) != 2 || out[0] != strings.TrimSpace(same) {
		t.Fatalf("got %#v", out)
	}
}

