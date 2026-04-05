package profile

import (
	"testing"
)

func TestGenerateAndParse(t *testing.T) {
	raw := Generate("185.1.2.3:56000", "mypass", "hy2")
	if raw[:5] != "gt://" {
		t.Fatalf("prefix: got %q", raw[:5])
	}

	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Addr != "185.1.2.3:56000" {
		t.Errorf("Addr: %q", p.Addr)
	}
	if p.Password != "mypass" {
		t.Errorf("Password: %q", p.Password)
	}
	if p.SNI != "hy2" {
		t.Errorf("SNI: %q", p.SNI)
	}
}

func TestParseWithoutPrefix(t *testing.T) {
	raw := Generate("1.2.3.4:443", "test", "example.com")
	p, err := Parse(raw[5:]) // strip gt://
	if err != nil {
		t.Fatalf("Parse without prefix: %v", err)
	}
	if p.Addr != "1.2.3.4:443" {
		t.Errorf("Addr: %q", p.Addr)
	}
}

func TestParseWithBOM(t *testing.T) {
	raw := "\uFEFF  gt://" + Generate("1.2.3.4:443", "pass", "hy2")[5:]
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse with BOM: %v", err)
	}
	if p.Addr != "1.2.3.4:443" {
		t.Errorf("Addr: %q", p.Addr)
	}
}

func TestParseUnicode(t *testing.T) {
	raw := Generate("10.0.0.1:56000", "пароль-юникод", "hy2")
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse unicode: %v", err)
	}
	if p.Password != "пароль-юникод" {
		t.Errorf("Password: %q", p.Password)
	}
}

func TestParseInvalid(t *testing.T) {
	if _, err := Parse("not-base64!!!"); err == nil {
		t.Error("expected error for garbage")
	}
	if _, err := Parse("gt://bm90LWpzb24="); err == nil { // "not-json"
		t.Error("expected error for invalid JSON")
	}
	if _, err := Parse(""); err == nil {
		t.Error("expected error for empty")
	}
	if _, err := Parse("   "); err == nil {
		t.Error("expected error for whitespace only")
	}
}

func TestValidate(t *testing.T) {
	valid := Profile{Addr: "1.2.3.4:56000", Password: "pass", SNI: "hy2"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid profile: %v", err)
	}

	noAddr := Profile{Password: "pass"}
	if err := noAddr.Validate(); err == nil {
		t.Error("expected error for missing addr")
	}

	noPass := Profile{Addr: "1.2.3.4:56000"}
	if err := noPass.Validate(); err == nil {
		t.Error("expected error for missing password")
	}

	badAddr := Profile{Addr: "no-port", Password: "pass"}
	if err := badAddr.Validate(); err == nil {
		t.Error("expected error for bad addr format")
	}
}

func TestSNIDefaults(t *testing.T) {
	p := Profile{Addr: "1.2.3.4:443", Password: "pass"}
	if p.SNIOrDefault() != "hy2" {
		t.Errorf("default SNI: %q", p.SNIOrDefault())
	}
	if !p.IsSelfSigned() {
		t.Error("empty SNI should be self-signed")
	}

	p.SNI = "example.com"
	if p.SNIOrDefault() != "example.com" {
		t.Errorf("custom SNI: %q", p.SNIOrDefault())
	}
	if p.IsSelfSigned() {
		t.Error("custom SNI should not be self-signed")
	}
}
