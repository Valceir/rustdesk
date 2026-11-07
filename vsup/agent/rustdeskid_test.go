package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/secretbox"
)

func TestParseEncID(t *testing.T) {
	guid := "4f1c2d3e-aaaa-bbbb-cccc-0123456789ab"
	var key [32]byte
	copy(key[:], guid)
	var nonce [24]byte
	_, _ = rand.Read(nonce[:])
	box := secretbox.Seal(nil, []byte("377782843"), &nonce, &key)
	payload := append(append([]byte{1}, nonce[:]...), box...)
	toml := "enc_id = '00" + base64.StdEncoding.EncodeToString(payload) + "'\npassword = ''\n"
	if got := parseRustdeskID([]byte(toml), guid); got != "377782843" {
		t.Fatalf("v1: got %q", got)
	}
	var zero [24]byte
	legacy := secretbox.Seal(nil, []byte("123456789"), &zero, &key)
	toml = "enc_id = \"00" + base64.StdEncoding.EncodeToString(legacy) + "\"\n"
	if got := parseRustdeskID([]byte(toml), guid); got != "123456789" {
		t.Fatalf("legacy: got %q", got)
	}
	if got := parseRustdeskID([]byte("id = '987654321'\n"), guid); got != "987654321" {
		t.Fatalf("plain: got %q", got)
	}
	if got := parseRustdeskID([]byte(toml), "outra-chave"); got != "" {
		t.Fatalf("wrong key should fail, got %q", got)
	}
}

func TestDecodeConsole(t *testing.T) {
	// "Configuração" em CP850
	cp := []byte{'C', 'o', 'n', 'f', 'i', 'g', 'u', 'r', 'a', 0x87, 0xC6, 'o'}
	if got := decodeConsole(cp); got != "Configuração" {
		t.Fatalf("cp850: %q", got)
	}
	u16 := []byte{'O', 0, 'k', 0, '\r', 0, '\n', 0}
	if got := decodeConsole(u16); got != "Ok\n" {
		t.Fatalf("utf16: %q", got)
	}
}

func TestPingRejectsInjection(t *testing.T) {
	for _, bad := range []string{"8.8.8.8 & del x", "-t", "a|b", "x;y"} {
		if _, err := actPing(t.Context(), map[string]string{"target": bad}); err == nil {
			t.Fatalf("aceitou %q", bad)
		}
	}
}
