//go:build !unix || windows

package updater

func isExecReplaceBusy(err error) bool {
	return false
}
