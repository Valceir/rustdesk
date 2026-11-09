//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// No macOS a lista é outra: otimizar disco, sfc e Windows Update não existem; entra a busca do softwareupdate.
func init() {
	actions = map[string]action{
		"clean_temp":        {10 * time.Minute, macCleanTemp},
		"ip_info":           {30 * time.Second, macIPInfo},
		"flush_dns":         {30 * time.Second, macFlushDNS},
		"ping":              {60 * time.Second, macPing},
		"clear_print_queue": {2 * time.Minute, macClearPrintQueue},
		"mac_update":        {10 * time.Minute, macUpdate},
		"reboot":            {30 * time.Second, macReboot},
		"shutdown":          {30 * time.Second, macShutdown},
	}
}

// No macOS um arquivo aberto pode ser apagado sem erro (diferente do Windows), então só sai o que
// ninguém mexe há mais de um dia.
func macCleanTemp(ctx context.Context, _ map[string]string) (string, error) {
	dirs, _ := filepath.Glob("/private/var/folders/*/*/T")
	cutoff := time.Now().Add(-24 * time.Hour)
	var freed int64
	var removed int
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil || p == dir {
				return nil
			}
			fi, err := d.Info()
			if err != nil || !fi.Mode().IsRegular() || fi.ModTime().After(cutoff) {
				return nil
			}
			if os.Remove(p) == nil {
				freed += fi.Size()
				removed++
			}
			return nil
		})
	}
	return fmt.Sprintf("Liberados %s em %d arquivos temporários com mais de 1 dia.\nArquivos recentes foram mantidos.", human(freed), removed), nil
}

func macIPInfo(ctx context.Context, _ map[string]string) (string, error) {
	var b strings.Builder
	for _, c := range [][]string{{"scutil", "--nwi"}, {"route", "-n", "get", "default"}, {"scutil", "--dns"}} {
		out, _ := runCmd(ctx, c[0], c[1:]...)
		if c[0] == "scutil" && c[1] == "--dns" {
			if i := strings.Index(out, "resolver #2"); i > 0 {
				out = out[:i]
			}
		}
		fmt.Fprintf(&b, "$ %s\n%s\n\n", strings.Join(c, " "), strings.TrimSpace(out))
	}
	return b.String(), nil
}

func macFlushDNS(ctx context.Context, _ map[string]string) (string, error) {
	if out, err := runCmd(ctx, "dscacheutil", "-flushcache"); err != nil {
		return out, err
	}
	if out, err := runCmd(ctx, "killall", "-HUP", "mDNSResponder"); err != nil {
		return out, err
	}
	return "Cache de DNS limpo.", nil
}

func macPing(ctx context.Context, args map[string]string) (string, error) {
	target := strings.TrimSpace(args["target"])
	if target == "" {
		target = "8.8.8.8"
	}
	if !hostRe.MatchString(target) {
		return "", fmt.Errorf("endereço inválido: %q", target)
	}
	return runCmd(ctx, "ping", "-c", "4", target)
}

func macClearPrintQueue(ctx context.Context, _ map[string]string) (string, error) {
	jobs, _ := runCmd(ctx, "lpstat", "-o")
	n := 0
	for _, l := range strings.Split(jobs, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	if out, err := runCmd(ctx, "cancel", "-a", "-"); err != nil {
		return out, err
	}
	return fmt.Sprintf("%d trabalho(s) de impressão cancelado(s).", n), nil
}

func macUpdate(ctx context.Context, _ map[string]string) (string, error) {
	out, err := runCmd(ctx, "softwareupdate", "--list")
	if err != nil {
		return out, err
	}
	return out + "\n\nPara instalar, use Ajustes do Sistema → Geral → Atualização de Software no Mac.", nil
}

func macReboot(ctx context.Context, _ map[string]string) (string, error) {
	notifyConsoleUser(shutdownMsg)
	out, err := runCmd(ctx, "shutdown", "-r", "+1")
	if err == nil {
		out = "O Mac vai reiniciar em 1 minuto (o usuário recebeu um aviso)."
	}
	return out, err
}

func macShutdown(ctx context.Context, _ map[string]string) (string, error) {
	notifyConsoleUser(strings.Replace(shutdownMsg, "reiniciar", "desligar", 1))
	out, err := runCmd(ctx, "shutdown", "-h", "+1")
	if err == nil {
		out = "O Mac vai desligar em 1 minuto (o usuário recebeu um aviso)."
	}
	return out, err
}

// notifyConsoleUser mostra um alerta na sessão de quem está usando o Mac; some sozinho em 55 s.
func notifyConsoleUser(msg string) {
	uid := cmdOut("stat", "-f", "%u", "/dev/console")
	user := cmdOut("stat", "-f", "%Su", "/dev/console")
	if uid == "" || uid == "0" || user == "" {
		return
	}
	script := fmt.Sprintf(`display alert "VDesk" message %q giving up after 55`, msg)
	cmd := exec.Command("/bin/launchctl", "asuser", uid, "/usr/bin/sudo", "-u", user, "/usr/bin/osascript", "-e", script)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}
