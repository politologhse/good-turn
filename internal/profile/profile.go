// Package profile handles gt:// connection profile strings.
// A profile encodes server address, password, and SNI into a shareable string.
// Format: gt://base64({"a":"addr","p":"pass","s":"sni"})
package profile

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// Profile represents a Good TURN connection profile.
type Profile struct {
	Addr     string `json:"a"` // Relay server address (host:port)
	Password string `json:"p"` // Hysteria2 password
	SNI      string `json:"s"` // TLS Server Name Indication
}

const prefix = "gt://"

// Generate creates a gt:// profile string.
func Generate(addr, password, sni string) string {
	p := Profile{Addr: addr, Password: password, SNI: sni}
	b, _ := json.Marshal(p)
	return prefix + base64.StdEncoding.EncodeToString(b)
}

// Parse decodes a gt:// profile string. Handles invisible chars, BOM, whitespace.
func Parse(raw string) (Profile, error) {
	// Strip BOM, zero-width chars, whitespace
	cleaned := strings.Map(func(r rune) rune {
		if r == '\uFEFF' || r == '\u200B' || r == '\u200C' || r == '\u200D' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	// Strip gt:// prefix (find it anywhere in case of leading garbage)
	if idx := strings.Index(cleaned, prefix); idx != -1 {
		cleaned = cleaned[idx+len(prefix):]
	}

	// Strip any trailing whitespace after prefix removal
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		return Profile{}, fmt.Errorf("empty profile string")
	}

	b, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return Profile{}, fmt.Errorf("invalid base64: %w", err)
	}

	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, fmt.Errorf("invalid JSON: %w", err)
	}

	return p, nil
}

// Validate checks that a profile has all required fields and valid format.
func (p Profile) Validate() error {
	if p.Addr == "" {
		return fmt.Errorf("server address is required")
	}
	if p.Password == "" {
		return fmt.Errorf("password is required")
	}

	// Validate addr is host:port
	_, _, err := net.SplitHostPort(p.Addr)
	if err != nil {
		return fmt.Errorf("invalid server address %q: %w", p.Addr, err)
	}

	return nil
}

// SNIOrDefault returns SNI or "hy2" if empty.
func (p Profile) SNIOrDefault() string {
	if p.SNI == "" {
		return "hy2"
	}
	return p.SNI
}

// IsSelfSigned returns true if the SNI indicates a self-signed cert.
func (p Profile) IsSelfSigned() bool {
	return p.SNIOrDefault() == "hy2"
}
