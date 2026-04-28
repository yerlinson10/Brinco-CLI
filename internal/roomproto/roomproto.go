package roomproto

import "strings"

const (
	ProtocolP2P        = "p2p"
	ProtocolDirect     = "direct"
	ProtocolRelay      = "relay"
	ProtocolGuaranteed = "guaranteed"
)

var known = map[string]bool{
	ProtocolP2P:        true,
	ProtocolDirect:     true,
	ProtocolRelay:      true,
	ProtocolGuaranteed: true,
}

func IsKnown(protocol string) bool {
	return known[strings.ToLower(strings.TrimSpace(protocol))]
}

func Wrap(protocol, payload string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	if !IsKnown(p) {
		return payload
	}
	return p + "-" + strings.TrimSpace(payload)
}

func Unwrap(code string) (protocol string, payload string) {
	c := strings.TrimSpace(code)
	idx := strings.IndexByte(c, '-')
	if idx <= 0 {
		return "", c
	}
	p := strings.ToLower(strings.TrimSpace(c[:idx]))
	if !IsKnown(p) {
		return "", c
	}
	return p, strings.TrimSpace(c[idx+1:])
}
