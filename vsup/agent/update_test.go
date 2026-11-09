package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestAllowedURL(t *testing.T) {
	for _, u := range []string{
		"https://github.com/Valceir/rustdesk/releases/download/vsup-1.6.0/VSUP-1.6.0-windows.exe",
		"https://release-assets.githubusercontent.com/x",
	} {
		if err := allowedURL(u); err != nil {
			t.Errorf("%s recusado: %v", u, err)
		}
	}
	for _, u := range []string{"http://github.com/x.exe", "https://exemplo.com/VSUP.exe", "https://github.com.evil.io/x.exe", "file:///c:/x.exe"} {
		if allowedURL(u) == nil {
			t.Errorf("%s deveria ser recusado", u)
		}
	}
}

func TestDownloadChecksSHA256(t *testing.T) {
	body := []byte("instalador de teste")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	host := strings.Split(strings.TrimPrefix(srv.URL, "https://"), ":")[0]
	updateHosts[host] = true
	updateTransport = srv.Client().Transport
	t.Setenv("TMPDIR", t.TempDir())
	defer func() { delete(updateHosts, host); updateTransport = nil }()

	u, _ := url.JoinPath(srv.URL, "VSUP-9.9.9-windows.exe")
	good := sha256.Sum256(body)
	path, err := download(u, hex.EncodeToString(good[:]))
	if err != nil {
		t.Fatalf("hash certo recusado: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(body) {
		t.Fatal("conteúdo baixado diferente")
	}
	if _, err := download(u, strings.Repeat("0", 64)); err == nil {
		t.Fatal("hash errado foi aceito")
	}
}
