//go:build !unix || windows

package updater

func scheduleDeferredPOSIXReplace(exePath, newPath string) error {
	return nil
}
