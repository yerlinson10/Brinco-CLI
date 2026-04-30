//go:build unix && !windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// scheduleDeferredPOSIXReplace ejecuta en segundo plano: espera a que termine este proceso
// y luego mueve newPath sobre exePath (mv -f).
func scheduleDeferredPOSIXReplace(exePath, newPath string) error {
	pid := os.Getpid()
	script := `set -eu
while kill -0 "${BRINCO_UPDATE_WAIT_PID}" 2>/dev/null; do sleep 0.25; done
sleep 1
exec mv -f "${BRINCO_UPDATE_NEW}" "${BRINCO_UPDATE_DEST}"
`
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(),
		"BRINCO_UPDATE_WAIT_PID="+strconv.Itoa(pid),
		"BRINCO_UPDATE_NEW="+newPath,
		"BRINCO_UPDATE_DEST="+exePath,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("iniciar reemplazo aplazado: %w", err)
	}
	return nil
}
