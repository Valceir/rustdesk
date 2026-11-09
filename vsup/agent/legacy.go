package main

import "errors"

// O VDesk substitui o VSUP. O próprio VDesk copia a configuração do VSUP (ID, chaves, senhas) na primeira
// execução; o agente só remove o VSUP depois de ver o VDesk com o mesmo ID. Até lá não se registra, para os
// dois agentes não disputarem o mesmo ID no painel.
var legacyChecked bool

func (a *agent) removeLegacy() error {
	if legacyChecked {
		return nil
	}
	if !legacyInstalled() {
		legacyChecked = true
		return nil
	}
	id := readRustdeskID()
	if id == "" {
		return errors.New("VDesk ainda sem ID: o VSUP fica no PC até o VDesk subir")
	}
	if old := readIDFrom(configCandidatesFor(LegacyName)); old != "" && old != id {
		legacyChecked = true
		logf("o ID do VDesk (%s) é diferente do ID do VSUP (%s): o VSUP NÃO foi removido", id, old)
		return nil
	}
	logf("VDesk com o ID %s: removendo o VSUP", id)
	if err := uninstallLegacy(); err != nil {
		logf("remover o VSUP: %v", err)
	} else {
		logf("VSUP removido")
	}
	legacyChecked = true
	return nil
}
