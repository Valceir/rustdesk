package main

import (
	"encoding/base64"
	"regexp"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"
)

var encIDRe = regexp.MustCompile(`(?m)^enc_id\s*=\s*['"]([^'"]+)['"]`)

// parseRustdeskID lê o ID do VSUP.toml. O VSUP grava o ID criptografado em enc_id
// ("00" + base64 de secretbox, chave = MachineGuid do Windows); versões antigas usam id em texto.
func parseRustdeskID(data []byte, machineUID string) string {
	if m := idRe.FindSubmatch(data); m != nil {
		if id := strings.TrimSpace(string(m[1])); id != "" {
			return id
		}
	}
	m := encIDRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return decryptEncID(string(m[1]), machineUID)
}

func decryptEncID(enc, machineUID string) string {
	if len(enc) <= 2 || !strings.HasPrefix(enc, "00") || machineUID == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(enc[2:])
	if err != nil {
		return ""
	}
	var key [32]byte
	copy(key[:], machineUID) // mesmo que o VSUP: bytes do MachineGuid, completados/cortados em 32
	const formatV1 = 1
	if len(raw) >= 1+24+secretbox.Overhead && raw[0] == formatV1 {
		var nonce [24]byte
		copy(nonce[:], raw[1:25])
		if out, ok := secretbox.Open(nil, raw[25:], &nonce, &key); ok {
			return strings.TrimSpace(string(out))
		}
	}
	var zero [24]byte
	if out, ok := secretbox.Open(nil, raw, &zero, &key); ok {
		return strings.TrimSpace(string(out))
	}
	return ""
}
