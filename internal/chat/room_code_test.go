package chat

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRoomCodeRoundTripDirect(t *testing.T) {
	code, err := BuildRoomCode("127.0.0.1:10000", "room-abc")
	if err != nil {
		t.Fatalf("BuildRoomCode() error = %v", err)
	}
	got, err := ParseRoomCodeDetailed(code)
	if err != nil {
		t.Fatalf("ParseRoomCodeDetailed() error = %v", err)
	}
	if got.Mode != roomModeDirect {
		t.Fatalf("mode = %q, want %q", got.Mode, roomModeDirect)
	}
	if got.Addr != "127.0.0.1:10000" || got.Room != "room-abc" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestRoomCodeRoundTripGuaranteedProtocol(t *testing.T) {
	code, err := BuildRelayRoomCode("10.0.0.2:10000", "room-xyz")
	if err != nil {
		t.Fatalf("BuildRelayRoomCode() error = %v", err)
	}
	code = RewriteCodeProtocol(code, "guaranteed")
	got, err := ParseRoomCodeDetailed(code)
	if err != nil {
		t.Fatalf("ParseRoomCodeDetailed() error = %v", err)
	}
	if got.Mode != roomModeGuaranteed {
		t.Fatalf("mode = %q, want %q", got.Mode, roomModeGuaranteed)
	}
	if firstString(got.relayCandidates()) != "10.0.0.2:10000" || got.Room != "room-xyz" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestRoomCodeRejectsInvalidAndLegacyPayload(t *testing.T) {
	if _, err := ParseRoomCodeDetailed("direct-not_base64"); err == nil {
		t.Fatalf("expected error for invalid payload")
	}

	legacyJSON := []byte(`{"mode":"direct","addr":"127.0.0.1:10000","room":"legacy"}`)
	legacyCode := "direct-" + base64.RawURLEncoding.EncodeToString(legacyJSON)
	if _, err := ParseRoomCodeDetailed(legacyCode); err == nil {
		t.Fatalf("expected error for legacy v1 payload")
	}
}

func TestRoomCodeNewPayloadShorterThanLegacy(t *testing.T) {
	payload := roomCodePayload{
		Mode:  roomModeRelay,
		Relay: "relay-us-east.example.com:10000,relay-us-west.example.com:10000,relay-eu.example.com:10000",
		Room:  "room-compact-check-with-repeated-relays",
	}
	newCode, err := BuildRelayRoomCode(payload.Relay, payload.Room)
	if err != nil {
		t.Fatalf("BuildRelayRoomCode() error = %v", err)
	}
	legacyRaw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	legacyCode := "relay-" + base64.RawURLEncoding.EncodeToString(legacyRaw)
	if len(newCode) >= len(legacyCode) {
		t.Fatalf("new code should be shorter: new=%d legacy=%d", len(newCode), len(legacyCode))
	}
}

func TestSplitRelayListDedupesCSV(t *testing.T) {
	got := SplitRelayList("a:1, b:2; a:1")
	if len(got) != 2 || got[0] != "a:1" || got[1] != "b:2" {
		t.Fatalf("SplitRelayList() = %#v", got)
	}
}

func TestBuildFileChunksMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(path, []byte("hello chunked transfer"), 0o600); err != nil {
		t.Fatal(err)
	}
	chunks, total, checksum, name, err := buildFileChunks(path)
	if err != nil {
		t.Fatalf("buildFileChunks() error = %v", err)
	}
	if total != 22 || checksum == "" || name != "demo.txt" || len(chunks) != 1 {
		t.Fatalf("unexpected chunks: total=%d checksum=%q name=%q len=%d", total, checksum, name, len(chunks))
	}
	if tid := chunks[0].TransferID; len(tid) < 32 || tid == checksum[:16] {
		t.Fatalf("transferId should be nonce+hash prefix, got len=%d tid=%q", len(tid), tid)
	}
	if !validFileChunk(chunks[0]) {
		t.Fatalf("chunk should be valid: %#v", chunks[0])
	}
}
