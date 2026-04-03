// Package creds fetches TURN credentials from VK API.
package creds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/google/uuid"
)

// LogFunc is a callback for log messages.
type LogFunc func(string)

// GetCredsFunc fetches TURN credentials: username, password, address.
type GetCredsFunc func(string) (string, string, string, error)

// WithRetry wraps a GetCredsFunc with exponential backoff retry.
func WithRetry(f GetCredsFunc, maxAttempts int, logf LogFunc) GetCredsFunc {
	return func(link string) (string, string, string, error) {
		var lastErr error
		backoff := 2 * time.Second
		for i := 0; i < maxAttempts; i++ {
			user, pass, addr, err := f(link)
			if err == nil {
				return user, pass, addr, nil
			}
			lastErr = err
			if i < maxAttempts-1 {
				logf(fmt.Sprintf("Creds attempt %d/%d failed: %s, retrying in %s...", i+1, maxAttempts, err, backoff))
				time.Sleep(backoff)
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
		return "", "", "", fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
	}
}

// ParseTurnURL extracts host:port from a TURN URL like "turn:1.2.3.4:3478?transport=udp".
func ParseTurnURL(raw string) string {
	clean := strings.Split(raw, "?")[0]
	return strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
}

// getString safely extracts a nested string from a JSON map.
func getString(m map[string]interface{}, keys ...string) (string, error) {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("expected object at key %q", k)
		}
		cur = mm[k]
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("expected string at %v, got %T", keys, cur)
	}
	return s, nil
}

func doHTTPPost(data, url, userAgent string, transport http.RoundTripper) (map[string]interface{}, error) {
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", userAgent)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0"

// GetVkCreds fetches TURN credentials from VK call link.
func GetVkCreds(link string, dialer *dnsdialer.Dialer, logf LogFunc) (string, string, string, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialer.DialContext,
	}

	// Step 1: get anonymous token
	resp, err := doHTTPPost(
		"client_id=6287487&token_type=messages&client_secret=QbYic1K3lEV5kTGiqlq2&version=1&app_id=6287487",
		"https://login.vk.ru/?act=get_anonym_token",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("VK anon token request: %w", err)
	}
	token1, err := getString(resp, "data", "access_token")
	if err != nil {
		return "", "", "", fmt.Errorf("VK anon token parse: %w", err)
	}

	// Step 2: get call token
	resp, err = doHTTPPost(
		fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=123&access_token=%s", link, token1),
		"https://api.vk.ru/method/calls.getAnonymousToken?v=5.274&client_id=6287487",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("VK call token request: %w", err)
	}
	token2, err := getString(resp, "response", "token")
	if err != nil {
		return "", "", "", fmt.Errorf("VK call token parse: %w", err)
	}

	// Step 3: OK session
	resp, err = doHTTPPost(
		fmt.Sprintf("session_data=%%7B%%22version%%22%%3A2%%2C%%22device_id%%22%%3A%%22%s%%22%%2C%%22client_version%%22%%3A1.1%%2C%%22client_type%%22%%3A%%22SDK_JS%%22%%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", uuid.New()),
		"https://calls.okcdn.ru/fb.do",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("OK session request: %w", err)
	}
	token3, err := getString(resp, "session_key")
	if err != nil {
		return "", "", "", fmt.Errorf("OK session parse: %w", err)
	}

	// Step 4: join conference, get TURN creds
	resp, err = doHTTPPost(
		fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3),
		"https://calls.okcdn.ru/fb.do",
		defaultUserAgent, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("TURN creds request: %w", err)
	}

	user, err := getString(resp, "turn_server", "username")
	if err != nil {
		return "", "", "", fmt.Errorf("TURN username: %w", err)
	}
	pass, err := getString(resp, "turn_server", "credential")
	if err != nil {
		return "", "", "", fmt.Errorf("TURN credential: %w", err)
	}

	turnServer, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", "", fmt.Errorf("missing turn_server in response")
	}
	urls, ok := turnServer["urls"].([]interface{})
	if !ok || len(urls) == 0 {
		return "", "", "", fmt.Errorf("missing turn_server urls")
	}
	turnURL, ok := urls[0].(string)
	if !ok {
		return "", "", "", fmt.Errorf("turn_server url is not a string")
	}

	return user, pass, ParseTurnURL(turnURL), nil
}
