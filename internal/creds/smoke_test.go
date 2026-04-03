package creds

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVkCredsEndToEndMock simulates the full 4-step VK credential flow
// against a mock HTTP server to verify the request/response chain works.
func TestVkCredsEndToEndMock(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])

		switch {
		case strings.Contains(r.URL.String(), "get_anonym_token"):
			// Step 1: anon token
			step++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"access_token": "tok1"},
			})

		case strings.Contains(r.URL.String(), "calls.getAnonymousToken"):
			// Step 2: call token — verify link was passed
			step++
			if !strings.Contains(bodyStr, "vk_join_link=") {
				t.Error("missing vk_join_link in request body")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{"token": "tok2"},
			})

		case strings.Contains(bodyStr, "auth.anonymLogin"):
			// Step 3: OK session
			step++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"session_key": "sess3",
			})

		case strings.Contains(bodyStr, "vchat.joinConversationByLink"):
			// Step 4: join + TURN creds
			step++
			if !strings.Contains(bodyStr, "anonymToken=tok2") {
				t.Error("step 4: missing anonymToken")
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turn_server": map[string]interface{}{
					"username":   "turn-user",
					"credential": "turn-pass",
					"urls":       []interface{}{"turn:10.20.30.40:3478?transport=udp"},
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()

	// We can't test GetVkCreds directly because URLs are hardcoded.
	// But we CAN test the parsing chain by simulating each step's response.
	// The mock above verifies the request structure at each step.

	// Instead, test that doHTTPPost works correctly with our mock
	resp, err := doHTTPPost("key=val", srv.URL+"/login?act=get_anonym_token", defaultUserAgent, http.DefaultTransport)
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	tok1, err := getString(resp, "data", "access_token")
	if err != nil || tok1 != "tok1" {
		t.Fatalf("step 1 parse: tok=%q err=%v", tok1, err)
	}

	resp, err = doHTTPPost("vk_join_link=test&access_token="+tok1, srv.URL+"/method/calls.getAnonymousToken", defaultUserAgent, http.DefaultTransport)
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}
	tok2, err := getString(resp, "response", "token")
	if err != nil || tok2 != "tok2" {
		t.Fatalf("step 2 parse: tok=%q err=%v", tok2, err)
	}

	resp, err = doHTTPPost("method=auth.anonymLogin", srv.URL+"/fb.do", defaultUserAgent, http.DefaultTransport)
	if err != nil {
		t.Fatalf("step 3: %v", err)
	}
	sess, err := getString(resp, "session_key")
	if err != nil || sess != "sess3" {
		t.Fatalf("step 3 parse: sess=%q err=%v", sess, err)
	}

	resp, err = doHTTPPost("method=vchat.joinConversationByLink&anonymToken=tok2&session_key=sess3", srv.URL+"/fb.do", defaultUserAgent, http.DefaultTransport)
	if err != nil {
		t.Fatalf("step 4: %v", err)
	}
	user, err := getString(resp, "turn_server", "username")
	if err != nil || user != "turn-user" {
		t.Fatalf("step 4 user: %q err=%v", user, err)
	}
	pass, err := getString(resp, "turn_server", "credential")
	if err != nil || pass != "turn-pass" {
		t.Fatalf("step 4 pass: %q err=%v", pass, err)
	}

	ts, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		t.Fatal("no turn_server")
	}
	urls := ts["urls"].([]interface{})
	addr := ParseTurnURL(urls[0].(string))
	if addr != "10.20.30.40:3478" {
		t.Fatalf("addr: %q", addr)
	}

	if step != 4 {
		t.Errorf("expected 4 steps, got %d", step)
	}
}
