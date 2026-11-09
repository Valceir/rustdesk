//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const updateNote = " No Mac, confira se Gravação de Tela e Acessibilidade continuam liberadas para o VSUP."

// installUpdate monta o .dmg, troca o VSUP.app no lugar e reinicia o VSUP. O agente sai em seguida e o
// launchd sobe o agente novo, que informa o resultado.
func installUpdate(dmg string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // .../VSUP.app/Contents/MacOS/vsup-agent
	if !strings.HasSuffix(app, ".app") {
		return fmt.Errorf("agente fora de um .app: %s", exe)
	}

	mnt := filepath.Join(updateDir(), "mnt")
	_ = os.MkdirAll(mnt, 0o700)
	if out, err := exec.Command("/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		return fmt.Errorf("montar o .dmg: %v %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("/usr/bin/hdiutil", "detach", mnt, "-force").Run()

	src := filepath.Join(mnt, filepath.Base(app))
	if _, err := os.Stat(filepath.Join(src, "Contents", "MacOS", "vsup-agent")); err != nil {
		return fmt.Errorf("o .dmg não tem o %s esperado", filepath.Base(app))
	}
	staged := app + ".novo"
	_ = os.RemoveAll(staged)
	if out, err := exec.Command("/usr/bin/ditto", src, staged).CombinedOutput(); err != nil {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("copiar o app: %v %s", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("/usr/bin/xattr", "-dr", "com.apple.quarantine", staged).Run()

	uiRunning := exec.Command("/usr/bin/pgrep", "-x", appProcess(app)).Run() == nil
	old := app + ".antigo"
	_ = os.RemoveAll(old)
	if err := os.Rename(app, old); err != nil {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("trocar o app: %w", err)
	}
	if err := os.Rename(staged, app); err != nil {
		_ = os.Rename(old, app)
		return fmt.Errorf("trocar o app: %w", err)
	}
	_ = os.RemoveAll(old)

	restartVSUP(app, uiRunning)
	go func() {
		time.Sleep(3 * time.Second)
		os.Exit(0) // launchd (KeepAlive) sobe o agente novo
	}()
	return nil
}

func appProcess(app string) string { return strings.TrimSuffix(filepath.Base(app), ".app") }

// restartVSUP reinicia o serviço (root) e o servidor da sessão do usuário. Sem o serviço instalado
// (acesso só com aceite), reabre o app para o usuário, senão o PC ficaria sem acesso até alguém abrir.
func restartVSUP(app string, uiRunning bool) {
	daemons, _ := filepath.Glob("/Library/LaunchDaemons/*" + appProcess(app) + "_service.plist")
	agents, _ := filepath.Glob("/Library/LaunchAgents/*" + appProcess(app) + "_server.plist")
	uid := cmdOut("stat", "-f", "%u", "/dev/console")
	user := cmdOut("stat", "-f", "%Su", "/dev/console")
	label := func(p string) string { return strings.TrimSuffix(filepath.Base(p), ".plist") }

	_ = exec.Command("/usr/bin/pkill", "-x", appProcess(app)).Run()
	for _, d := range daemons {
		_ = launchctl("kickstart", "-k", "system/"+label(d))
	}
	if uid != "" && uid != "0" {
		for _, a := range agents {
			_ = launchctl("kickstart", "-k", "gui/"+uid+"/"+label(a))
		}
		if len(daemons) == 0 && uiRunning && user != "" {
			_ = exec.Command("/bin/launchctl", "asuser", uid, "/usr/bin/sudo", "-u", user, "/usr/bin/open", "-a", app).Run()
		}
	}
	if len(daemons) == 0 && len(agents) == 0 && !uiRunning {
		logf("VSUP não estava aberto: abre na próxima vez que o usuário iniciar")
	}
}
