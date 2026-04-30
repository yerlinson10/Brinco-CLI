package roomcode

import (
	"encoding/base64"
	"strings"
	"testing"
)

type testRoomCode struct {
	RoomID string `msgpack:"room_id"`
	User   string `msgpack:"user"`
	Nonce  int64  `msgpack:"nonce"`
}

func TestEncodeDecode_RoundTrip(t *testing.T) {
	in := testRoomCode{
		RoomID: "alpha-room",
		User:   "deman",
		Nonce:  42,
	}

	payload, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var out testRoomCode
	if err := Decode(payload, &out); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if out != in {
		t.Fatalf("decoded value mismatch: got %#v want %#v", out, in)
	}
}

func TestDecode_InputValidation(t *testing.T) {
	var out testRoomCode

	tests := []struct {
		name    string
		payload string
		target  any
		wantErr string
	}{
		{
			name:    "empty payload",
			payload: "",
			target:  &out,
			wantErr: errEmptyPayload.Error(),
		},
		{
			name:    "nil target",
			payload: "abc",
			target:  nil,
			wantErr: errNilOutput.Error(),
		},
		{
			name:    "non pointer target",
			payload: "abc",
			target:  out,
			wantErr: errInvalidOutput.Error(),
		},
		{
			name:    "nil pointer target",
			payload: "abc",
			target:  (*testRoomCode)(nil),
			wantErr: errInvalidOutput.Error(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Decode(tc.payload, tc.target)
			if err == nil {
				t.Fatalf("Decode() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Decode() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestDecode_InvalidBase64(t *testing.T) {
	var out testRoomCode
	err := Decode("%%%not-base64%%%", &out)
	if err == nil {
		t.Fatal("Decode() error = nil, want base64 decode error")
	}
	if !strings.Contains(err.Error(), "decode base64 payload") {
		t.Fatalf("Decode() error = %q, want base64 context", err.Error())
	}
}

func TestDecode_PayloadTooLarge(t *testing.T) {
	huge := make([]byte, maxCompressedPayloadBytes+1)
	payload := base64.RawURLEncoding.EncodeToString(huge)

	var out testRoomCode
	err := Decode(payload, &out)
	if err == nil {
		t.Fatal("Decode() error = nil, want payload size error")
	}
	if !strings.Contains(err.Error(), "compressed payload too large") {
		t.Fatalf("Decode() error = %q, want size context", err.Error())
	}
}
