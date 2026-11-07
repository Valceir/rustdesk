package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const inventoryInterval = 6 * time.Hour

var errUnauthorized = errors.New("token recusado pelo painel")

type state struct {
	RustdeskID string `json:"rustdesk_id"`
	Token      string `json:"token"`
}

type command struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Args json.RawMessage `json:"args,omitempty"`
}

type agent struct {
	st      state
	http    *http.Client
	poll    *http.Client
	lastInv time.Time
}

// run é o laço principal; termina quando stop é fechado.
func run(stop <-chan struct{}) {
	logf("VSUP Agent %s iniciado", Version)
	a := &agent{
		http: &http.Client{Timeout: 60 * time.Second},
		poll: &http.Client{Timeout: 45 * time.Second},
	}
	a.st = loadState()
	backoff := 5 * time.Second
	for {
		select {
		case <-stop:
			logf("parando")
			return
		default:
		}
		if err := a.step(stop); err != nil {
			logf("%v (nova tentativa em %s)", err, backoff)
			if !sleep(stop, backoff) {
				return
			}
			if backoff < 5*time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Second
	}
}

// step: garante registro, envia inventário quando vence e espera comandos.
func (a *agent) step(stop <-chan struct{}) error {
	id := readRustdeskID()
	if id == "" {
		return errors.New("ID do VSUP ainda não disponível")
	}
	if a.st.Token == "" || a.st.RustdeskID != id {
		if err := a.register(id); err != nil {
			return fmt.Errorf("registro: %w", err)
		}
		a.lastInv = time.Time{}
	}
	if time.Since(a.lastInv) > inventoryInterval {
		if err := a.sendInventory(); err != nil {
			return a.handleErr("inventário", err)
		}
	}
	cmds, err := a.waitCommands()
	if err != nil {
		return a.handleErr("comandos", err)
	}
	for _, c := range cmds {
		a.execute(c)
	}
	return nil
}

func (a *agent) handleErr(what string, err error) error {
	if errors.Is(err, errUnauthorized) {
		a.st.Token = ""
		saveState(a.st)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (a *agent) register(id string) error {
	host, _ := os.Hostname()
	var resp struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	err := a.call("POST", "/api/agent/register", "", map[string]string{
		"id": id, "machine_guid": machineGUID(), "hostname": host, "version": Version,
	}, &resp)
	if err != nil {
		return err
	}
	if resp.Token == "" {
		return errors.New("painel não devolveu token")
	}
	a.st = state{RustdeskID: id, Token: resp.Token}
	saveState(a.st)
	logf("registrado no painel com o ID %s", id)
	return nil
}

func (a *agent) sendInventory() error {
	inv := collectInventory()
	if err := a.call("POST", "/api/agent/inventory", a.st.Token, inv, nil); err != nil {
		return err
	}
	a.lastInv = time.Now()
	logf("inventário enviado")
	return nil
}

func (a *agent) waitCommands() ([]command, error) {
	var resp struct {
		Commands    []command `json:"commands"`
		LiveSeconds int       `json:"live_seconds"`
	}
	req, err := http.NewRequest("GET", PanelURL+"/api/agent/poll", nil)
	if err != nil {
		return nil, err
	}
	a.headers(req, a.st.Token)
	r, err := a.poll.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if err := decode(r, &resp); err != nil {
		return nil, err
	}
	if resp.LiveSeconds > 0 {
		a.startLive(resp.LiveSeconds)
	}
	return resp.Commands, nil
}

func (a *agent) execute(c command) {
	logf("comando %s (%s)", c.Type, c.ID)
	if c.Type == "inventory" {
		ok, output := true, "Inventário atualizado"
		if err := a.sendInventory(); err != nil {
			ok, output = false, err.Error()
		}
		a.report(c.ID, ok, output)
		return
	}
	// Ações podem demorar (sfc, otimização de disco): rodam em paralelo sem travar o recebimento.
	token := a.st.Token
	go func() {
		ok, output := runAction(c)
		logf("comando %s (%s) terminou: ok=%v", c.Type, c.ID, ok)
		a.reportWith(token, c.ID, ok, output)
	}()
}

func (a *agent) report(id string, ok bool, output string) {
	a.reportWith(a.st.Token, id, ok, output)
}

func (a *agent) reportWith(token, id string, ok bool, output string) {
	body := map[string]any{"id": id, "ok": ok, "output": output}
	for i := 0; i < 5; i++ {
		err := a.call("POST", "/api/agent/result", token, body, nil)
		if err == nil {
			return
		}
		logf("resultado do comando %s: %v", id, err)
		time.Sleep(time.Duration(i+1) * 10 * time.Second)
	}
}

func (a *agent) headers(req *http.Request, token string) {
	req.Header.Set("User-Agent", "VSUP-Agent/"+Version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (a *agent) call(method, path, token string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, PanelURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.headers(req, token)
	r, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return decode(r, out)
}

func decode(r *http.Response, out any) error {
	data, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if r.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if r.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("HTTP %d: %s", r.StatusCode, e.Error)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func sleep(stop <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}

// ---------- ID do VSUP ----------

var idRe = regexp.MustCompile(`(?m)^id\s*=\s*['"]([^'"]+)['"]`)

// readRustdeskID lê o ID do arquivo de configuração do serviço do VSUP.
func readRustdeskID() string {
	if id := os.Getenv("VSUP_TEST_ID"); id != "" {
		return id
	}
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = `C:\Windows`
	}
	candidates := []string{
		filepath.Join(win, `ServiceProfiles\LocalService\AppData\Roaming\VSUP\config\VSUP.toml`),
		filepath.Join(win, `System32\config\systemprofile\AppData\Roaming\VSUP\config\VSUP.toml`),
	}
	if d, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(d, `VSUP\config\VSUP.toml`))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if id := parseRustdeskID(data, machineGUID()); id != "" {
			return id
		}
	}
	return ""
}

// ---------- estado e log ----------

// O token fica no perfil do SYSTEM, que usuários comuns não leem.
func stateDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "VSUP")
}

func loadState() state {
	var s state
	data, err := os.ReadFile(filepath.Join(stateDir(), "agent.json"))
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveState(s state) {
	_ = os.MkdirAll(stateDir(), 0o700)
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(stateDir(), "agent.json"), b, 0o600); err != nil {
		logf("salvar estado: %v", err)
	}
}

var logToConsole bool

func logf(format string, args ...any) {
	line := fmt.Sprintf("%s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	if logToConsole {
		fmt.Print(line)
		return
	}
	dir := filepath.Join(programData(), "VSUP")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "agent.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func programData() string {
	if d := os.Getenv("ProgramData"); d != "" {
		return d
	}
	return `C:\ProgramData`
}
