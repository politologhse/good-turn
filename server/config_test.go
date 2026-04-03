package main

import (
	"testing"
)

func TestGenerateAndParseConfigString(t *testing.T) {
	addr := "185.1.2.3:56000"
	pass := "mypassword"
	sni := "hy2"

	cs := generateConfigString(addr, pass, sni)

	// Must start with gt://
	if cs[:5] != "gt://" {
		t.Fatalf("config string must start with gt://, got %q", cs[:5])
	}

	// Parse back
	data, err := parseConfigString(cs)
	if err != nil {
		t.Fatalf("parseConfigString: %v", err)
	}
	if data.Addr != addr {
		t.Errorf("addr: got %q, want %q", data.Addr, addr)
	}
	if data.Pass != pass {
		t.Errorf("pass: got %q, want %q", data.Pass, pass)
	}
	if data.SNI != sni {
		t.Errorf("sni: got %q, want %q", data.SNI, sni)
	}
}

func TestParseConfigStringWithoutPrefix(t *testing.T) {
	cs := generateConfigString("1.2.3.4:443", "test", "example.com")
	raw := cs[5:] // strip gt://

	data, err := parseConfigString(raw)
	if err != nil {
		t.Fatalf("parse without prefix: %v", err)
	}
	if data.Addr != "1.2.3.4:443" {
		t.Errorf("addr: got %q", data.Addr)
	}
}

func TestParseConfigStringInvalid(t *testing.T) {
	_, err := parseConfigString("not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}

	_, err = parseConfigString("gt://bm90LWpzb24=") // "not-json"
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestConfigStringUnicodePassword(t *testing.T) {
	cs := generateConfigString("10.0.0.1:56000", "пароль-с-юникодом", "hy2")
	data, err := parseConfigString(cs)
	if err != nil {
		t.Fatalf("parse unicode: %v", err)
	}
	if data.Pass != "пароль-с-юникодом" {
		t.Errorf("unicode pass: got %q", data.Pass)
	}
}
