//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	legacyApp   = "/Applications/" + LegacyName + ".app"
	legacyLabel = "br.com.vsasolutions.vsup.agent"
)

func legacyInstalled() bool {
	if _, err := os.Stat(legacyApp); err == nil {
		return true
	}
	_, err := os.Stat("/Library/LaunchDaemons/" + legacyLabel + ".plist")
	return err == nil
}

// uninstallLegacy tira do launchd o serviço, o servidor da sessão e o agente do VSUP e apaga o VSUP.app.
// Se o VSUP tinha o serviço (acesso sem aceite) e o VDesk ainda não, instala o do VDesk no lugar.
func uninstallLegacy() error {
	label := func(p string) string { return strings.TrimSuffix(filepath.Base(p), ".plist") }
	uid := cmdOut("stat", "-f", "%u", "/dev/console")
	daemons, _ := filepath.Glob("/Library/LaunchDaemons/*" + LegacyName + "_service.plist")
	agents, _ := filepath.Glob("/Library/LaunchAgents/*" + LegacyName + "_server.plist")
	for _, d := range daemons {
		_ = launchctl("bootout", "system/"+label(d))
		_ = os.Remove(d)
	}
	for _, a := range agents {
		if uid != "" && uid != "0" {
			_ = launchctl("bootout", "gui/"+uid+"/"+label(a))
		}
		_ = os.Remove(a)
	}
	_ = os.Remove("/Library/LaunchDaemons/" + legacyLabel + ".plist")
	_ = launchctl("bootout", "system/"+legacyLabel)
	_ = exec.Command("/usr/bin/pkill", "-x", LegacyName).Run()
	err := os.RemoveAll(legacyApp)

	if len(daemons) > 0 {
		if mine, _ := filepath.Glob("/Library/LaunchDaemons/*" + AppName + "_service.plist"); len(mine) == 0 {
			installAppService(uid)
		}
	}
	return err
}

func installAppService(uid string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	service := filepath.Join(filepath.Dir(exe), "service")
	if out, err := exec.Command(service, "--write-plists").CombinedOutput(); err != nil {
		logf("serviço do VDesk: %v %s", err, strings.TrimSpace(string(out)))
		return
	}
	daemon := "/Library/LaunchDaemons/com.carriez." + AppName + "_service.plist"
	agent := "/Library/LaunchAgents/com.carriez." + AppName + "_server.plist"
	if err := launchctl("bootstrap", "system", daemon); err != nil {
		logf("serviço do VDesk: %v", err)
	}
	if uid != "" && uid != "0" {
		_ = launchctl("bootstrap", "gui/"+uid, agent)
	}
	logf("serviço do VDesk instalado no lugar do serviço do VSUP")
}
