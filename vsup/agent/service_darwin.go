//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// No macOS o agente é um LaunchDaemon (root). O serviço do VDesk chama "vdesk-agent install" sempre que
// inicia, então a instalação e as atualizações do VDesk já deixam o agente na versão do app.
const (
	launchdLabel = "br.com.vsasolutions.vdesk.agent"
	plistPath    = "/Library/LaunchDaemons/" + launchdLabel + ".plist"
)

const plistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>30</integer>
</dict>
</plist>
`

// launchd inicia o agente com o pai = launchd (pid 1).
func isService() bool { return os.Getppid() == 1 }

func runService() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(stop)
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	exe, _ := os.Executable()
	start, _ := os.Stat(exe)
	check := time.NewTicker(time.Minute)
	defer check.Stop()
	for {
		select {
		case <-sig:
			close(stop)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
			}
			return
		case <-check.C:
			now, err := os.Stat(exe)
			switch {
			case errors.Is(err, os.ErrNotExist):
				// O VDesk.app foi apagado: o agente sai junto.
				logf("VDesk.app não existe mais: removendo o agente")
				_ = uninstallService()
				return
			case err == nil && start != nil && !os.SameFile(start, now):
				// VDesk atualizado (binário trocado): sai e o launchd sobe a versão nova.
				logf("VDesk atualizado: reiniciando o agente")
				close(stop)
				<-done
				os.Exit(0)
			}
		}
	}
}

func installService() error {
	if os.Geteuid() != 0 {
		return errors.New("precisa ser executado como root")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	body := fmt.Sprintf(plistTmpl, launchdLabel, exe)
	target := "system/" + launchdLabel
	if old, err := os.ReadFile(plistPath); err == nil && string(old) == body && launchctl("print", target) == nil {
		// Mesmo caminho: reinicia para rodar o binário novo depois de uma atualização do VDesk.
		return launchctl("kickstart", "-k", target)
	}
	_ = launchctl("bootout", target)
	if err := os.WriteFile(plistPath, []byte(body), 0o644); err != nil {
		return err
	}
	_ = os.Chown(plistPath, 0, 0)
	return launchctl("bootstrap", "system", plistPath)
}

func uninstallService() error {
	err := os.Remove(plistPath)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	_ = launchctl("bootout", "system/"+launchdLabel) // encerra este processo quando chamado pelo próprio agente
	return err
}

func launchctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

var platformUUIDRe = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

// machineGUID é o IOPlatformUUID, o mesmo que o VDesk usa para criptografar o ID.
func machineGUID() string {
	out, err := exec.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	if m := platformUUIDRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// O serviço roda como root (/var/root); o app do usuário guarda a mesma configuração na pasta dele.
func configCandidatesFor(app string) []string {
	var c []string
	for _, pat := range []string{
		"/var/root/Library/Preferences/*." + app + "/" + app + ".toml",
		"/Users/*/Library/Preferences/*." + app + "/" + app + ".toml",
	} {
		m, _ := filepath.Glob(pat)
		c = append(c, m...)
	}
	return c
}

func logDir() string { return logDirFor(AppName) }

func logDirFor(app string) string { return "/Library/Logs/" + app }
