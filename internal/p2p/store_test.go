package p2p

import (
	"errors"
	"os"
	"path/filepath"
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

