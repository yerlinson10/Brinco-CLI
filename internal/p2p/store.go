package p2p

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const lastCodeFile = "brinco-last-p2p-code.txt"

const lastPeersFile = "brinco-last-room-peers.json"

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

type peerMemoriesFile struct {
	Rooms map[string]string `json:"rooms"`
}

func lastPeersPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("no se pudo resolver directorio de cache del usuario")
	}
	return filepath.Join(dir, lastPeersFile), nil
}

func loadPeerMemories() (peerMemoriesFile, error) {
	path, err := lastPeersPath()
	if err != nil {
		return peerMemoriesFile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return peerMemoriesFile{Rooms: map[string]string{}}, nil
		}
		return peerMemoriesFile{}, err
	}
	var mem peerMemoriesFile
	if err := json.Unmarshal(raw, &mem); err != nil {
		return peerMemoriesFile{}, err
	}
	if mem.Rooms == nil {
		mem.Rooms = map[string]string{}
	}
	return mem, nil
}

func savePeerMemories(mem peerMemoriesFile) error {
	if mem.Rooms == nil {
		mem.Rooms = map[string]string{}
	}
	path, err := lastPeersPath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// saveLastWorkingPeer guarda el multiaddr de dial que funciono para un topic de sala.
func saveLastWorkingPeer(topic, multiaddr string) error {
	topic = strings.TrimSpace(topic)
	multiaddr = strings.TrimSpace(multiaddr)
	if topic == "" || multiaddr == "" {
		return errors.New("topic o multiaddr vacio")
	}
	mem, err := loadPeerMemories()
	if err != nil {
		return err
	}
	mem.Rooms[topic] = multiaddr
	return savePeerMemories(mem)
}

// loadLastWorkingPeer devuelve el multiaddr guardado para el topic, si existe.
func loadLastWorkingPeer(topic string) (string, bool) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "", false
	}
	mem, err := loadPeerMemories()
	if err != nil {
		return "", false
	}
	addr, ok := mem.Rooms[topic]
	addr = strings.TrimSpace(addr)
	return addr, ok && addr != ""
}

// orderPeersPreferStored coloca primero el ultimo peer que funciono en esta sala
// (mismo topic), sin duplicar entradas del codigo actual.
func orderPeersPreferStored(topic string, peers []string) (ordered []string, usedStored bool) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return append([]string{}, peers...), false
	}
	out := make([]string, 0, len(peers)+1)
	seen := make(map[string]struct{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	haveStored := false
	if last, ok := loadLastWorkingPeer(topic); ok {
		haveStored = true
		add(last)
	}
	for _, p := range peers {
		add(p)
	}
	return out, haveStored && len(out) > 0
}
