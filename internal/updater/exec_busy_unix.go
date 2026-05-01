//go:build unix && !windows

package updater

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

func isExecReplaceBusy(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, unix.ETXTBSY) || errors.Is(err, unix.EBUSY) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "text file busy") ||
		strings.Contains(msg, "resource busy")
}
