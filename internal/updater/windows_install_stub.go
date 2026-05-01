//go:build !windows

package updater

func scheduleWindowsSelfReplace(exePath, newPath string) error {
	return nil
}
