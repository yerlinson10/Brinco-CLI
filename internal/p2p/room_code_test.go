package p2p

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoomCodeRoundTripP2P(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooW111",
		"/ip4/127.0.0.1/tcp/4002/p2p/12D3KooW222",
	}
	code, err := BuildRoomCodeWithProtocol("topic-1", "", peers, "p2p")
	if err != nil {
		t.Fatalf("BuildRoomCodeWithProtocol() error = %v", err)
	}
	got, err := ParseRoomCodeDetailed(code)
	if err != nil {
		t.Fatalf("ParseRoomCodeDetailed() error = %v", err)
	}
	if got.Topic != "topic-1" {
		t.Fatalf("topic = %q, want topic-1", got.Topic)
	}
	if !reflect.DeepEqual(got.Peers, peers) {
		t.Fatalf("peers = %#v, want %#v", got.Peers, peers)
	}
}

func TestRoomCodeRoundTripGuaranteed(t *testing.T) {
	code, err := BuildRoomCodeWithProtocol("topic-g", "/ip4/1.2.3.4/tcp/4001", nil, "guaranteed")
	if err != nil {
		t.Fatalf("BuildRoomCodeWithProtocol() error = %v", err)
	}
	got, err := ParseRoomCodeDetailed(code)
	if err != nil {
		t.Fatalf("ParseRoomCodeDetailed() error = %v", err)
	}
	if got.Topic != "topic-g" || firstString(got.relayCandidates()) != "/ip4/1.2.3.4/tcp/4001" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestRoomCodeRejectsInvalidAndLegacyPayload(t *testing.T) {
	if _, err := ParseRoomCodeDetailed("p2p-not_base64"); err == nil {
		t.Fatalf("expected error for invalid payload")
	}

	legacyJSON := []byte(`{"topic":"legacy-topic","peers":["/ip4/127.0.0.1/tcp/4001/p2p/12D3KooW111"]}`)
	legacyCode := "p2p-" + base64.RawURLEncoding.EncodeToString(legacyJSON)
	if _, err := ParseRoomCodeDetailed(legacyCode); err == nil {
		t.Fatalf("expected error for legacy v1 payload")
	}
}

func TestRoomCodeNewPayloadShorterThanLegacy(t *testing.T) {
	payload := roomCodePayload{
		Topic: "topic-length-check",
		Peers: []string{
			"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooW111111111111111111111111111111111111111",
			"/ip4/127.0.0.1/tcp/4002/p2p/12D3KooW222222222222222222222222222222222222222",
		},
	}
	newCode, err := BuildRoomCodeWithProtocol(payload.Topic, "", payload.Peers, "guaranteed")
	if err != nil {
		t.Fatalf("BuildRoomCodeWithProtocol() error = %v", err)
	}
	legacyRaw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	legacyCode := "guaranteed-" + base64.RawURLEncoding.EncodeToString(legacyRaw)
	if len(newCode) >= len(legacyCode) {
		t.Fatalf("new code should be shorter: new=%d legacy=%d", len(newCode), len(legacyCode))
	}
}

func TestSplitRelayListDedupesCSV(t *testing.T) {
	got := splitRelayList("/ip4/1.1.1.1/tcp/4001/p2p/a, /ip4/2.2.2.2/tcp/4001/p2p/b; /ip4/1.1.1.1/tcp/4001/p2p/a")
	if len(got) != 2 {
		t.Fatalf("splitRelayList() = %#v", got)
	}
}

func TestSendFileRejectsOverLimit(t *testing.T) {
	old := maxP2PFileBytes
	maxP2PFileBytes = 4
	defer func() { maxP2PFileBytes = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "too-big.txt")
	if err := os.WriteFile(path, []byte("too big"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := &Node{name: "tester"}
	if err := n.sendFile(path); err == nil {
		t.Fatalf("expected file limit error")
	}
}
