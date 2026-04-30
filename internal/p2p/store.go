package p2p

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const lastCodeFile = "brinco-last-p2p-code.txt"
var ErrNoSavedCode = errors.New("no hay codigo guardado")

func saveLastCode(code string) error {
	path, err := lastCodePath()
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return ErrNoSavedCode
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(trimmed), 0o600)
}

func loadLastCode() (string, error) {
	path, err := lastCodePath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNoSavedCode
		}
		return "", err
	}
	code := strings.TrimSpace(string(raw))
	if code == "" {
		return "", ErrNoSavedCode
	}
	return code, nil
}

func lastCodePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("no se pudo resolver directorio de cache del usuario")
	}
	return filepath.Join(dir, lastCodeFile), nil
}
