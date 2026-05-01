//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// scheduleWindowsSelfReplace lanza PowerShell en segundo plano: espera a que termine
// este proceso y luego mueve newPath sobre exePath (el .exe ya no está bloqueado).
func scheduleWindowsSelfReplace(exePath, newPath string) error {
	waitPID := os.Getpid()
	exeQ := psSingleQuoted(exePath)
	newQ := psSingleQuoted(newPath)
	// $waitPid evita chocar con la variable automática $PID de PowerShell.
	script := fmt.Sprintf(
		`$ErrorActionPreference = 'Stop'; $waitPid = %d; $new = %s; $dest = %s; while ($null -ne (Get-Process -Id $waitPid -ErrorAction SilentlyContinue)) { Start-Sleep -Milliseconds 250 }; Start-Sleep -Seconds 1; Move-Item -LiteralPath $new -Destination $dest -Force`,
		waitPID, newQ, exeQ,
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("programar reemplazo en segundo plano: %w", err)
	}
	return nil
}

func psSingleQuoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
