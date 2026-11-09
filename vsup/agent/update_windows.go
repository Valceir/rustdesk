//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// installUpdate roda o instalador com --update, o mesmo caminho da atualização do VDesk: troca os arquivos
// sem desinstalar, mantém ID e configuração, reinicia o serviço e reabre o VDesk na sessão do usuário.
// O processo fica separado do agente, que é parado pelo próprio instalador.
func installUpdate(path string) error {
	const detachedProcess = 0x00000008
	cmd := exec.Command(path, "--update")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

const updateNote = ""
