package roomproto

import "testing"

func TestIsKnown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "exact p2p", input: ProtocolP2P, expected: true},
		{name: "spaces and uppercase", input: "  ReLaY ", expected: true},
		{name: "unknown", input: "foo", expected: false},
		{name: "empty", input: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsKnown(tt.input); got != tt.expected {
				t.Fatalf("IsKnown(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		payload  string
		expected string
	}{
		{name: "known protocol", protocol: " relay ", payload: " abc ", expected: "relay-abc"},
		{name: "unknown protocol returns payload", protocol: "foo", payload: " abc ", expected: " abc "},
		{name: "empty payload", protocol: ProtocolDirect, payload: " ", expected: "direct-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Wrap(tt.protocol, tt.payload); got != tt.expected {
				t.Fatalf("Wrap(%q, %q) = %q, want %q", tt.protocol, tt.payload, got, tt.expected)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	tests := []struct {
		name             string
		code             string
		expectedProtocol string
		expectedPayload  string
	}{
		{name: "known protocol", code: "relay-abc", expectedProtocol: "relay", expectedPayload: "abc"},
		{name: "known uppercase with spaces", code: "  RELAY - abc  ", expectedProtocol: "relay", expectedPayload: "abc"},
		{name: "no separator", code: "abc", expectedProtocol: "", expectedPayload: "abc"},
		{name: "separator at start", code: "-abc", expectedProtocol: "", expectedPayload: "-abc"},
		{name: "unknown protocol", code: "foo-abc", expectedProtocol: "", expectedPayload: "foo-abc"},
		{name: "empty payload", code: "relay-", expectedProtocol: "relay", expectedPayload: ""},
		{name: "payload with extra dashes", code: "relay-a-b-c", expectedProtocol: "relay", expectedPayload: "a-b-c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocol, payload := Unwrap(tt.code)
			if protocol != tt.expectedProtocol || payload != tt.expectedPayload {
				t.Fatalf(
					"Unwrap(%q) = (%q, %q), want (%q, %q)",
					tt.code,
					protocol,
					payload,
					tt.expectedProtocol,
					tt.expectedPayload,
				)
			}
		})
	}
}
