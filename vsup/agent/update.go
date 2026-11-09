package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Atualização do VDesk pelo painel. O instalador vem dos releases do GitHub e só roda se o SHA-256 bater
// com o que o painel informou. Como a instalação reinicia o próprio agente, o resultado é enviado pelo
// agente novo, a partir do arquivo de pendência.

const (
	maxInstallerSize = 600 << 20
	updateGiveUp     = 20 * time.Minute
)

var (
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionRe  = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	fileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,120}\.(exe|dmg)$`)
	// Releases do GitHub redirecionam para estes domínios.
	updateHosts = map[string]bool{
		"github.com":                           true,
		"objects.githubusercontent.com":        true,
		"release-assets.githubusercontent.com": true,
	}
)

var updateTransport http.RoundTripper // nil = padrão; trocado nos testes

type pendingUpdate struct {
	CommandID string    `json:"command_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        time.Time `json:"at"`
}

func pendingPath() string { return filepath.Join(logDir(), "update-pending.json") }

// Pendência deixada pelo agente do VSUP quando ele instalou o VDesk.
func legacyPendingPath() string { return filepath.Join(logDirFor(LegacyName), "update-pending.json") }
func updateDir() string         { return filepath.Join(logDir(), "update") }

func (a *agent) startUpdate(c command) {
	var args struct {
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(c.Args, &args)
	token := a.st.Token
	fail := func(err error) {
		logf("atualização %s: %v", c.ID, err)
		a.reportWith(token, c.ID, false, err.Error())
	}
	sum := strings.ToLower(strings.TrimSpace(args.SHA256))
	if !sha256Re.MatchString(sum) || !versionRe.MatchString(args.Version) {
		fail(errors.New("dados da atualização inválidos"))
		return
	}
	if args.Version == Version {
		a.reportWith(token, c.ID, true, "O VDesk já está na versão "+Version+".")
		return
	}
	if err := allowedURL(args.URL); err != nil {
		fail(err)
		return
	}
	go func() {
		path, err := download(args.URL, sum)
		if err != nil {
			fail(err)
			return
		}
		p := pendingUpdate{CommandID: c.ID, From: Version, To: args.Version, At: time.Now()}
		b, _ := json.Marshal(p)
		if err := os.WriteFile(pendingPath(), b, 0o600); err != nil {
			fail(fmt.Errorf("gravar pendência: %w", err))
			return
		}
		logf("instalando VDesk %s (de %s)", args.Version, Version)
		if err := installUpdate(path); err != nil {
			_ = os.Remove(pendingPath())
			fail(fmt.Errorf("instalador: %w", err))
		}
		// Daqui em diante o instalador para este agente; o resultado sai pelo agente novo.
	}()
}

func allowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !updateHosts[u.Hostname()] {
		return errors.New("endereço do instalador não permitido")
	}
	return nil
}

func download(raw, sum string) (string, error) {
	_ = os.RemoveAll(updateDir())
	if err := os.MkdirAll(updateDir(), 0o700); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "VDesk-Agent/"+Version)
	client := &http.Client{Transport: updateTransport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || !updateHosts[r.URL.Hostname()] {
			return errors.New("redirecionamento não permitido")
		}
		return nil
	}}
	r, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", r.StatusCode)
	}
	name := filepath.Base(req.URL.Path)
	if !fileNameRe.MatchString(name) {
		return "", errors.New("nome do instalador inválido")
	}
	path := filepath.Join(updateDir(), name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, maxInstallerSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if n > maxInstallerSize {
		return "", errors.New("instalador maior que o permitido")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		_ = os.Remove(path)
		return "", errors.New("o arquivo baixado não confere com a versão publicada (SHA-256 diferente); nada foi instalado")
	}
	return path, nil
}

// finishPendingUpdate roda depois do registro: informa ao painel como terminou a atualização.
func (a *agent) finishPendingUpdate() {
	if _, err := os.Stat(pendingPath()); err != nil {
		if b, err := os.ReadFile(legacyPendingPath()); err == nil {
			_ = os.MkdirAll(logDir(), 0o700)
			if os.WriteFile(pendingPath(), b, 0o600) == nil {
				_ = os.Remove(legacyPendingPath())
			}
		}
	}
	b, err := os.ReadFile(pendingPath())
	if err != nil {
		return
	}
	var p pendingUpdate
	if json.Unmarshal(b, &p) != nil || p.CommandID == "" {
		_ = os.Remove(pendingPath())
		return
	}
	switch {
	case Version == p.To:
		a.report(p.CommandID, true, fmt.Sprintf("Atualizado de %s para VDesk %s.", p.From, p.To)+updateNote)
	case time.Since(p.At) > updateGiveUp:
		a.report(p.CommandID, false, fmt.Sprintf("A instalação não terminou: o PC continua na versão %s.", Version))
	default:
		return // instalador ainda pode estar rodando
	}
	_ = os.Remove(pendingPath())
	_ = os.RemoveAll(updateDir())
}
