//go:build !windows

// Apenas para testes fora do Windows (o agente é distribuído só para Windows).
package main

import (
	"errors"
	"os"
	"strings"
)

func isService() bool         { return false }
func runService()             {}
func installService() error   { return errors.New("somente Windows") }
func uninstallService() error { return errors.New("somente Windows") }

func machineGUID() string {
	b, _ := os.ReadFile("/etc/machine-id")
	return strings.TrimSpace(string(b))
}
