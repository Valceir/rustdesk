//go:build !windows && !darwin

package main

import "errors"

func installUpdate(string) error {
	return errors.New("atualização disponível só no Windows e no macOS")
}

const updateNote = ""
