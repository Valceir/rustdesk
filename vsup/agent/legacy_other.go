//go:build !windows && !darwin

package main

func legacyInstalled() bool  { return false }
func uninstallLegacy() error { return nil }
