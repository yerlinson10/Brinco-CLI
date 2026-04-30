package chat

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"brinco-cli/internal/logx"
)

const (
	clientSendChanCap    = 128
	scannerTokenInitial  = 64 * 1024
	scannerTokenMax      = 512 * 1024
	relayProbeTimeout    = 900 * time.Millisecond
	handshakeReadTimeout = 45 * time.Second
	maxDecodeFailures    = 32
	uniqueNameMaxNumeric = 10000
	acceptErrBackoff     = 200 * time.Millisecond
	stdinLineChanCap     = 128
)

func pickSuccessorHost(clients map[*serverClient]struct{}) *serverClient {
	var best *serverClient
	for c := range clients {
		if best == nil || c.joinedAt.Before(best.joinedAt) {
			best = c
		}
	}
	return best
}

func allocUniqueNick(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	for i := 2; i < uniqueNameMaxNumeric; i++ {
		candidate := fmt.Sprintf("%s#%d", base, i)
		if !taken(candidate) {
			return candidate
		}
	}
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return fmt.Sprintf("%s#%d", base, time.Now().UnixNano())
		}
		candidate := fmt.Sprintf("%s#%s", base, hex.EncodeToString(b[:]))
		if !taken(candidate) {
			return candidate
		}
	}
}

func enqueueClientWire(c *serverClient, msg wireMessage, context string) bool {
	select {
	case c.send <- msg:
		return true
	default:
		logx.Warn("chat outbound queue full", "peer", c.name, "context", context)
		return false
	}
}

func acceptShouldStop(err error) bool {
	return err != nil && errors.Is(err, net.ErrClosed)
}
