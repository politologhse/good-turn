package main

import (
	"testing"
)

func TestGenerateAndParseConfigString(t *testing.T) {
	cs := generateConfigString("185.1.2.3:56000", "mypassword", "hy2")
	if cs[:5] != "gt://" {
		t.Fatalf("prefix: got %q", cs[:5])
	}

	p, err := parseConfigString(cs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Addr != "185.1.2.3:56000" {
		t.Errorf("addr: %q", p.Addr)
	}
	if p.Password != "mypassword" {
		t.Errorf("pass: %q", p.Password)
	}
	if p.SNI != "hy2" {
		t.Errorf("sni: %q", p.SNI)
	}
}

func TestParseConfigStringWithoutPrefix(t *testing.T) {
	cs := generateConfigString("1.2.3.4:443", "test", "example.com")
	p, err := parseConfigString(cs[5:]) // strip gt://
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Addr != "1.2.3.4:443" {
		t.Errorf("addr: %q", p.Addr)
	}
}

func TestParseConfigStringInvalid(t *testing.T) {
	if _, err := parseConfigString("not-base64!!!"); err == nil {
		t.Error("expected error for garbage")
	}
	if _, err := parseConfigString("gt://bm90LWpzb24="); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestConfigStringUnicodePassword(t *testing.T) {
	cs := generateConfigString("10.0.0.1:56000", "пароль-с-юникодом", "hy2")
	p, err := parseConfigString(cs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Password != "пароль-с-юникодом" {
		t.Errorf("pass: %q", p.Password)
	}
}
