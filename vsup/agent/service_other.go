//go:build !windows && !darwin

// Apenas para testes fora do Windows e do macOS (o agente é distribuído para esses dois).
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func isService() bool         { return false }
func runService()             {}
func installService() error   { return errors.New("somente Windows e macOS") }
func uninstallService() error { return errors.New("somente Windows e macOS") }

func machineGUID() string {
	b, _ := os.ReadFile("/etc/machine-id")
	return strings.TrimSpace(string(b))
}

func configCandidates() []string {
	d, _ := os.UserConfigDir()
	return []string{filepath.Join(d, "VSUP", "VSUP.toml")}
}

func logDir() string { return filepath.Join(os.TempDir(), "VSUP") }
