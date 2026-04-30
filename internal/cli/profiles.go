package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Profile guarda atajos locales (solo en tu maquina). No subas passwords a repositorios.
type Profile struct {
	Mode        string `json:"mode,omitempty"`
	Relay       string `json:"relay,omitempty"`
	Code        string `json:"code,omitempty"`
	Password    string `json:"password,omitempty"`
	Listen      string `json:"listen,omitempty"`
	Public      string `json:"public,omitempty"`
	Name        string `json:"name,omitempty"`
	NotifySound string `json:"notifySound,omitempty"`
	NotifyLevel string `json:"notifyLevel,omitempty"`
	FileLimit   string `json:"fileLimit,omitempty"`
}

// profiles.json es un objeto JSON: {"nombrePerfil": { ... }, ... }.

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// userConfigDir apunta a os.UserConfigDir; los tests pueden sustituirla.
var userConfigDir = os.UserConfigDir

// profilesConfigDir devuelve el directorio de configuracion (sin crearlo en disco).
func profilesConfigDir() (string, error) {
	d, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "brinco-cli"), nil
}

func profilesPath() (string, error) {
	dir, err := profilesConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles.json"), nil
}

// readProfilesFile lee profiles.json y asegura el directorio padre (0o700),
// para que ReadFile distinga bien archivo inexistente vs ruta invalida.
func readProfilesFile(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func parseProfilesBytes(raw []byte) (map[string]Profile, error) {
	raw = bytes.TrimPrefix(raw, utf8BOM)
	var all map[string]Profile
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = make(map[string]Profile)
	}
	return all, nil
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
	raw, err := readProfilesFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, fmt.Errorf("no existe profiles.json (%s); crea uno con tus atajos", path)
		}
		return Profile{}, err
	}
	all, err := parseProfilesBytes(raw)
	if err != nil {
		return Profile{}, fmt.Errorf("profiles.json invalido: %w", err)
	}
	p, ok := all[name]
	if !ok {
		return Profile{}, fmt.Errorf("perfil %q no encontrado", name)
	}
	return p, nil
}
