// Agente VSUP: inventário do PC e canal de comandos com o painel.
// Desenvolvido pela V.S.A. Solutions.
//
// Uso:
//
//	vsup-agent install     cria e inicia o serviço do Windows (chamado pelo instalador do VSUP)
//	vsup-agent uninstall   para e remove o serviço
//	vsup-agent inventory   mostra o inventário em JSON (teste)
//	vsup-agent debug       roda no console, sem serviço (teste)
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Preenchida na compilação: -ldflags "-X main.Version=1.0.0"
var Version = "dev"

const (
	ServiceName    = "VSUPAgent"
	ServiceDisplay = "VSUP Agent"
	ServiceDesc    = "Agente do VSUP: inventário e ações de suporte da V.S.A. Solutions."
	defaultPanel   = "https://vsup.vsasolutions.com.br"
)

// PanelURL pode ser trocado para testes com a variável VSUP_PANEL_URL (só administradores definem
// variáveis de ambiente do sistema).
var PanelURL = func() string {
	if u := os.Getenv("VSUP_PANEL_URL"); u != "" {
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
		// Nunca falha a instalação do VSUP por causa do agente.
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
		fmt.Println("VSUP Agent", Version, "- uso: vsup-agent install | uninstall | inventory | debug")
	}
}
