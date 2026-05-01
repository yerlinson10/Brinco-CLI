package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProfilesBytes_valid(t *testing.T) {
	all, err := parseProfilesBytes([]byte(`{"a":{"relay":"r1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if all["a"].Relay != "r1" {
		t.Fatalf("relay: got %q", all["a"].Relay)
	}
}

func TestParseProfilesBytes_BOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"x":{"mode":"p2p"}}`)...)
	all, err := parseProfilesBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if all["x"].Mode != "p2p" {
		t.Fatalf("mode: got %q", all["x"].Mode)
	}
}

func TestParseProfilesBytes_nullJSON(t *testing.T) {
	all, err := parseProfilesBytes([]byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty map, got %d keys", len(all))
	}
}

func TestLoadProfile(t *testing.T) {
	tmp := t.TempDir()
	prev := userConfigDir
	t.Cleanup(func() { userConfigDir = prev })
	userConfigDir = func() (string, error) { return tmp, nil }

	cfgDir := filepath.Join(tmp, "brinco-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "profiles.json")
	if err := os.WriteFile(path, []byte(`{"home":{"relay":"127.0.0.1:1","name":"me"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := loadProfile("home")
	if err != nil {
		t.Fatal(err)
	}
	if p.Relay != "127.0.0.1:1" || p.Name != "me" {
		t.Fatalf("profile: %+v", p)
	}

	_, err = loadProfile("nope")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}
