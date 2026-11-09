//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc/mgr"
)

const legacyService = "VSUPAgent"

func legacyExe() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, LegacyName, LegacyName+".exe")
}

func legacyInstalled() bool {
	if _, err := os.Stat(legacyExe()); err == nil {
		return true
	}
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(legacyService)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// uninstallLegacy roda o desinstalador do próprio VSUP (remove serviço, atalhos, pasta e o agente antigo)
// e garante que o serviço do agente antigo não ficou para trás.
func uninstallLegacy() error {
	var runErr error
	if _, err := os.Stat(legacyExe()); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, legacyExe(), "--uninstall")
		hideWindow(cmd)
		runErr = cmd.Run()
	}
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService(legacyService); err == nil {
			stopService(s)
			_ = s.Delete()
			s.Close()
		}
		m.Disconnect()
	}
	return runErr
}
