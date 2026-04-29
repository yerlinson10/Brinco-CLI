package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Profile guarda atajos locales (solo en tu maquina). No subas passwords a repositorios.
type Profile struct {
	Mode     string `json:"mode,omitempty"`
	Relay    string `json:"relay,omitempty"`
	Code     string `json:"code,omitempty"`
	Password string `json:"password,omitempty"`
	Listen   string `json:"listen,omitempty"`
	Public   string `json:"public,omitempty"`
	Name     string `json:"name,omitempty"`
}

func profilesDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "brinco-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func profilesPath() (string, error) {
	dir, err := profilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles.json"), nil
}

func loadProfile(name string) (Profile, error) {
	name = strings.TrimSpace(strings.TrimPrefix(name, "@"))
	if name == "" {
		return Profile{}, errors.New("nombre de perfil vacio")
	}
	path, err := profilesPath()
	if err != nil {
		return Profile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, fmt.Errorf("no existe profiles.json (%s); crea uno con tus atajos", path)
		}
		return Profile{}, err
	}
	var all map[string]Profile
	if err := json.Unmarshal(raw, &all); err != nil {
		return Profile{}, fmt.Errorf("profiles.json invalido: %w", err)
	}
	p, ok := all[name]
	if !ok {
		return Profile{}, fmt.Errorf("perfil %q no encontrado", name)
	}
	return p, nil
}
