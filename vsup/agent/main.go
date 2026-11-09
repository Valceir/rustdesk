// Agente VDesk (antigo VSUP): inventário do PC e canal de comandos com o painel.
// Desenvolvido pela V.S.A. Solutions.
//
// Uso:
//
//	vdesk-agent install     cria e inicia o serviço (chamado pelo instalador do VDesk)
//	vdesk-agent uninstall   para e remove o serviço
//	vdesk-agent inventory   mostra o inventário em JSON (teste)
//	vdesk-agent debug       roda no console, sem serviço (teste)
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Preenchida na compilação: -ldflags "-X main.Version=1.0.0"
var Version = "dev"

const (
	AppName        = "VDesk"
	LegacyName     = "VSUP" // nome antigo: o agente migra e remove o VSUP do PC
	ServiceName    = "VDeskAgent"
	ServiceDisplay = "VDesk Agent"
	ServiceDesc    = "Agente do VDesk: inventário e ações de suporte da V.S.A. Solutions."
	defaultPanel   = "https://vdesk.vsasolutions.com.br"
)

// PanelURL pode ser trocado para testes com a variável VDESK_PANEL_URL (só administradores definem
// variáveis de ambiente do sistema).
var PanelURL = func() string {
	if u := os.Getenv("VDESK_PANEL_URL"); u != "" {
		return u
	}
	return defaultPanel
}()

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "install":
		// Nunca falha a instalação do VDesk por causa do agente.
		if err := installService(); err != nil {
			logf("install: %v", err)
			fmt.Println("aviso:", err)
		}
		os.Exit(0)
	case "uninstall":
		if err := uninstallService(); err != nil {
			logf("uninstall: %v", err)
			fmt.Println("aviso:", err)
		}
		os.Exit(0)
	case "inventory":
		inv := collectInventory()
		b, _ := json.MarshalIndent(inv, "", "  ")
		fmt.Println(string(b))
	case "debug":
		logToConsole = true
		stop := make(chan struct{})
		run(stop)
	case "version", "--version":
		fmt.Println(Version)
	default:
		if isService() {
			runService()
			return
		}
		fmt.Println("VDesk Agent", Version, "- uso: vdesk-agent install | uninstall | inventory | debug")
	}
}
