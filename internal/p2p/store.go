package p2p

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const lastCodeFile = "brinco-last-p2p-code.txt"

func saveLastCode(code string) error {
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, lastCodeFile), []byte(strings.TrimSpace(code)), 0o600)
}

func loadLastCode() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(dir, lastCodeFile))
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(string(raw))
	if code == "" {
		return "", errors.New("no hay codigo guardado")
	}
	return code, nil
}
