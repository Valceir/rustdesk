package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Ações que o painel pode pedir. Qualquer outro tipo é recusado: o painel nunca envia comando livre.
type action struct {
	timeout time.Duration
	run     func(ctx context.Context, args map[string]string) (string, error)
}

var actions = map[string]action{
	"clean_temp":        {10 * time.Minute, actCleanTemp},
	"optimize_disk":     {2 * time.Hour, actOptimizeDisk},
	"ip_info":           {30 * time.Second, actIPInfo},
	"flush_dns":         {30 * time.Second, actFlushDNS},
	"ping":              {60 * time.Second, actPing},
	"clear_print_queue": {2 * time.Minute, actClearPrintQueue},
	"sfc":               {2 * time.Hour, actSFC},
	"windows_update":    {2 * time.Minute, actWindowsUpdate},
	"reboot":            {30 * time.Second, actReboot},
	"shutdown":          {30 * time.Second, actShutdown},
}

// Uma execução por tipo de ação ao mesmo tempo (ex.: dois sfc seguidos).
var running sync.Map

func runAction(c command) (bool, string) {
	act, ok := actions[c.Type]
	if !ok {
		return false, "Ação não suportada nesta versão do agente (" + Version + ")"
	}
	if _, busy := running.LoadOrStore(c.Type, true); busy {
		return false, "Esta ação já está em execução neste PC"
	}
	defer running.Delete(c.Type)

	args := map[string]string{}
	if len(c.Args) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(c.Args, &raw); err == nil {
			for k, v := range raw {
				args[k] = fmt.Sprint(v)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), act.timeout)
	defer cancel()
	out, err := act.run(ctx, args)
	out = strings.TrimSpace(out)
	if ctx.Err() == context.DeadlineExceeded {
		return false, strings.TrimSpace(out + "\n\nTempo limite atingido (" + act.timeout.String() + ").")
	}
	if err != nil {
		if out != "" {
			return false, out + "\n\n" + err.Error()
		}
		return false, err.Error()
	}
	return true, out
}

// ---------- ações ----------

func actCleanTemp(ctx context.Context, _ map[string]string) (string, error) {
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = `C:\Windows`
	}
	dirs := []string{filepath.Join(win, "Temp")}
	users, _ := filepath.Glob(`C:\Users\*\AppData\Local\Temp`)
	dirs = append(dirs, users...)

	var freed int64
	var removed, kept int
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				break
			}
			p := filepath.Join(dir, e.Name())
			size := dirSize(p)
			if err := os.RemoveAll(p); err != nil {
				kept++
				continue
			}
			freed += size
			removed++
		}
	}
	return fmt.Sprintf("Liberados %s em %d itens.\n%d itens em uso foram mantidos.\nPastas: %s",
		human(freed), removed, kept, strings.Join(dirs, ", ")), nil
}

func actOptimizeDisk(ctx context.Context, _ map[string]string) (string, error) {
	// /C todos os volumes, /O otimização adequada a cada tipo (TRIM no SSD, desfragmentação no HDD)
	return runCmd(ctx, "defrag", "/C", "/O")
}

func actIPInfo(ctx context.Context, _ map[string]string) (string, error) {
	return runCmd(ctx, "ipconfig", "/all")
}

func actFlushDNS(ctx context.Context, _ map[string]string) (string, error) {
	return runCmd(ctx, "ipconfig", "/flushdns")
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-:]{0,252}$`)

func actPing(ctx context.Context, args map[string]string) (string, error) {
	target := strings.TrimSpace(args["target"])
	if target == "" {
		target = "8.8.8.8"
	}
	if !hostRe.MatchString(target) {
		return "", fmt.Errorf("endereço inválido: %q", target)
	}
	return runCmd(ctx, "ping", "-n", "4", target)
}

func actClearPrintQueue(ctx context.Context, _ map[string]string) (string, error) {
	var b strings.Builder
	out, _ := runCmd(ctx, "net", "stop", "spooler", "/y")
	b.WriteString(out + "\n")
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = `C:\Windows`
	}
	files, _ := filepath.Glob(filepath.Join(win, `System32\spool\PRINTERS\*`))
	n := 0
	for _, f := range files {
		if os.Remove(f) == nil {
			n++
		}
	}
	fmt.Fprintf(&b, "%d arquivo(s) da fila removido(s).\n", n)
	out, err := runCmd(ctx, "net", "start", "spooler")
	b.WriteString(out)
	return b.String(), err
}

func actSFC(ctx context.Context, _ map[string]string) (string, error) {
	return runCmd(ctx, "sfc", "/scannow")
}

func actWindowsUpdate(ctx context.Context, _ map[string]string) (string, error) {
	out, err := runCmd(ctx, "usoclient", "StartScan")
	if err != nil {
		return out, err
	}
	return "Busca por atualizações iniciada. O Windows baixa e instala conforme a política do PC.", nil
}

const shutdownMsg = "O suporte da V.S.A. Solutions vai reiniciar este computador em 1 minuto. Salve seu trabalho."

func actReboot(ctx context.Context, _ map[string]string) (string, error) {
	out, err := runCmd(ctx, "shutdown", "/r", "/t", "60", "/c", shutdownMsg)
	if err == nil {
		out = "O PC vai reiniciar em 60 segundos (o usuário recebeu um aviso)."
	}
	return out, err
}

func actShutdown(ctx context.Context, _ map[string]string) (string, error) {
	out, err := runCmd(ctx, "shutdown", "/s", "/t", "60", "/c",
		strings.Replace(shutdownMsg, "reiniciar", "desligar", 1))
	if err == nil {
		out = "O PC vai desligar em 60 segundos (o usuário recebeu um aviso)."
	}
	return out, err
}

// ---------- utilitários ----------

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	text := decodeConsole(out)
	if len(text) > 20000 {
		text = text[:8000] + "\n...\n" + text[len(text)-12000:]
	}
	if err != nil {
		return text, fmt.Errorf("%s terminou com erro: %v", name, err)
	}
	return text, nil
}

// decodeConsole converte a saída dos comandos do Windows: UTF-16 (sfc), UTF-8 ou CP850 (padrão do console pt-BR).
func decodeConsole(b []byte) string {
	if len(b) >= 4 && (b[1] == 0 && b[3] == 0 || b[0] == 0xFF && b[1] == 0xFE) {
		if b[0] == 0xFF && b[1] == 0xFE {
			b = b[2:]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return cleanText(string(utf16.Decode(u)))
	}
	if utf8.Valid(b) {
		return cleanText(string(b))
	}
	r := make([]rune, len(b))
	for i, c := range b {
		if c < 0x80 {
			r[i] = rune(c)
		} else {
			r[i] = cp850[c-0x80]
		}
	}
	return cleanText(string(r))
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

var cp850 = []rune("ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜø£Ø×ƒáíóúñÑªº¿®¬½¼¡«»░▒▓│┤ÁÂÀ©╣║╗╝¢¥┐└┴┬├─┼ãÃ╚╔╩╦╠═╬¤ðÐÊËÈıÍÎÏ┘┌█▄¦Ì▀ÓßÔÒõÕµþÞÚÛÙýÝ¯´\u00AD±‗¾¶§÷¸°¨·¹³²■\u00A0")

func dirSize(p string) int64 {
	var total int64
	_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n/1024)
	}
}
