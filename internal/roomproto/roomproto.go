package roomproto

import "strings"

const (
	// ProtocolP2P identifies peer-to-peer room codes.
	ProtocolP2P        = "p2p"
	// ProtocolDirect identifies direct room codes.
	ProtocolDirect     = "direct"
	// ProtocolRelay identifies relay room codes.
	ProtocolRelay      = "relay"
	// ProtocolGuaranteed identifies guaranteed delivery room codes.
	ProtocolGuaranteed = "guaranteed"
)

var known = map[string]bool{
	ProtocolP2P:        true,
	ProtocolDirect:     true,
	ProtocolRelay:      true,
	ProtocolGuaranteed: true,
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

// IsKnown reports whether protocol is a known room protocol.
func IsKnown(protocol string) bool {
	return known[normalizeProtocol(protocol)]
}

// Wrap prefixes payload as "<protocol>-<payload>" when protocol is known.
// Unknown protocols return payload unchanged.
func Wrap(protocol, payload string) string {
	p := normalizeProtocol(protocol)
	if !known[p] {
		return payload
	}
	return p + "-" + strings.TrimSpace(payload)
}

// Unwrap splits a room code using the first '-' separator.
// It returns ("", code) when no known protocol prefix is present.
func Unwrap(code string) (protocol string, payload string) {
	c := strings.TrimSpace(code)
	idx := strings.IndexByte(c, '-')
	if idx <= 0 {
		return "", c
	}
	p := normalizeProtocol(c[:idx])
	if !known[p] {
		return "", c
	}
	return p, strings.TrimSpace(c[idx+1:])
}
