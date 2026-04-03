package creds

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseTurnURL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"turn:1.2.3.4:3478?transport=udp", "1.2.3.4:3478"},
		{"turns:5.6.7.8:5349?transport=tcp", "5.6.7.8:5349"},
		{"turn:example.com:3478", "example.com:3478"},
		{"turn:10.0.0.1:443", "10.0.0.1:443"},
	}
	for _, tt := range tests {
		got := ParseTurnURL(tt.input)
		if got != tt.want {
			t.Errorf("ParseTurnURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "hello",
		},
		"top": "world",
	}

	s, err := getString(m, "a", "b")
	if err != nil || s != "hello" {
		t.Errorf("getString(a.b) = %q, err=%v", s, err)
	}

	s, err = getString(m, "top")
	if err != nil || s != "world" {
		t.Errorf("getString(top) = %q, err=%v", s, err)
	}

	_, err = getString(m, "missing")
	if err == nil {
		t.Error("expected error for missing key")
	}

	_, err = getString(m, "a", "missing")
	if err == nil {
		t.Error("expected error for missing nested key")
	}
}

func TestGetStringTypeError(t *testing.T) {
	m := map[string]interface{}{"num": 42}
	_, err := getString(m, "num")
	if err == nil {
		t.Error("expected error for non-string value")
	}
}

func TestWithRetrySuccess(t *testing.T) {
	calls := 0
	f := func(link string) (string, string, string, error) {
		calls++
		if calls < 3 {
			return "", "", "", fmt.Errorf("fail %d", calls)
		}
		return "user", "pass", "addr", nil
	}

	var logs []string
	logf := func(msg string) { logs = append(logs, msg) }

	wrapped := WithRetry(f, 3, logf)
	user, pass, addr, err := wrapped("test")

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if user != "user" || pass != "pass" || addr != "addr" {
		t.Errorf("got %q %q %q", user, pass, addr)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 retry logs, got %d", len(logs))
	}
}

func TestWithRetryAllFail(t *testing.T) {
	f := func(link string) (string, string, string, error) {
		return "", "", "", fmt.Errorf("always fails")
	}

	start := time.Now()
	_, _, _, err := WithRetry(f, 2, func(string) {})(("test"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed < 1*time.Second {
		t.Errorf("expected backoff, elapsed %s", elapsed)
	}
}

func TestDoHTTPPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type: %s", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	resp, err := doHTTPPost("key=val", srv.URL, "test-agent", http.DefaultTransport)
	if err != nil {
		t.Fatalf("doHTTPPost: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("unexpected response: %v", resp)
	}
}

func TestVkCredsParsingMock(t *testing.T) {
	// Test that VK response structure is parsed correctly
	resp := map[string]interface{}{
		"turn_server": map[string]interface{}{
			"username":   "user123",
			"credential": "pass456",
			"urls":       []interface{}{"turn:10.0.0.1:3478?transport=udp"},
		},
	}

	user, err := getString(resp, "turn_server", "username")
	if err != nil || user != "user123" {
		t.Errorf("username: %q, err=%v", user, err)
	}

	cred, err := getString(resp, "turn_server", "credential")
	if err != nil || cred != "pass456" {
		t.Errorf("credential: %q, err=%v", cred, err)
	}

	ts, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		t.Fatal("turn_server not a map")
	}
	urls, ok := ts["urls"].([]interface{})
	if !ok || len(urls) == 0 {
		t.Fatal("urls missing")
	}
	addr := ParseTurnURL(urls[0].(string))
	if addr != "10.0.0.1:3478" {
		t.Errorf("addr: %q", addr)
	}
}
