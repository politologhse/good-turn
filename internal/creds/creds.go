// Package creds fetches TURN credentials from VK API.
package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/google/uuid"
)

// classifyNetError returns a user-friendly description of network errors.
func classifyNetError(err error, host string) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return fmt.Sprintf("DNS lookup failed for %s — check internet connection or DNS settings", host)
	case strings.Contains(s, "connection refused"):
		return fmt.Sprintf("%s refused connection — server may be down", host)
	case strings.Contains(s, "i/o timeout") || strings.Contains(s, "deadline exceeded"):
		return fmt.Sprintf("%s connection timed out — check firewall or network", host)
	case strings.Contains(s, "certificate"):
		return fmt.Sprintf("%s TLS error — certificate issue", host)
	default:
		return fmt.Sprintf("%s: %s", host, s)
	}
}

// LogFunc is a callback for log messages.
type LogFunc func(string)

// GetCredsFunc fetches TURN credentials: username, password, address.
type GetCredsFunc func(string) (string, string, string, error)

// WithRetry wraps a GetCredsFunc with exponential backoff retry.
// CaptchaManualNeeded errors are NOT retried — they propagate immediately for the GUI to handle.
func WithRetry(f GetCredsFunc, maxAttempts int, logf LogFunc) GetCredsFunc {
	return func(link string) (string, string, string, error) {
		var lastErr error
		backoff := 5 * time.Second
		for i := 0; i < maxAttempts; i++ {
			user, pass, addr, err := f(link)
			if err == nil {
				return user, pass, addr, nil
			}
			// Don't retry manual captcha — propagate for GUI handling
			var ce *CaptchaError
			if errors.As(err, &ce) && ce.Kind == CaptchaManualNeeded {
				return "", "", "", err
			}
			lastErr = err
			if i < maxAttempts-1 {
				logf(fmt.Sprintf("Attempt %d/%d failed: %s, retrying in %s...", i+1, maxAttempts, err, backoff))
				time.Sleep(backoff)
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
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

func doHTTPPost(data, url string, bp BrowserProfile, transport http.RoundTripper) (map[string]interface{}, error) {
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
	if err != nil {
		return nil, err
	}
	bp.ApplyHeaders(req)
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

// GetVkCreds fetches TURN credentials from VK call link.
func GetVkCreds(link string, dialer *dnsdialer.Dialer, logf LogFunc) (string, string, string, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialer.DialContext,
	}

	bp := RandomProfile()
	name := neturl.QueryEscape(randomName())
	logf(fmt.Sprintf("Identity: %s", name))

	// Step 1: get anonymous token
	resp, err := doHTTPPost(
		"client_id=6287487&token_type=messages&client_secret=QbYic1K3lEV5kTGiqlq2&version=1&app_id=6287487",
		"https://login.vk.ru/?act=get_anonym_token",
		bp, transport,
	)
	if err != nil {
		return "", "", "", fmt.Errorf("VK auth: %s", classifyNetError(err, "login.vk.ru"))
	}
	token1, err := getString(resp, "data", "access_token")
	if err != nil {
		return "", "", "", fmt.Errorf("VK anon token parse: %w", err)
	}

	// Step 2: get call token (with captcha retry loop)
	data := fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, name, token1)

	const maxCaptchaAttempts = 3
	var token2 string
	for attempt := 0; attempt <= maxCaptchaAttempts; attempt++ {
		resp, err = doHTTPPost(
			data,
			"https://api.vk.ru/method/calls.getAnonymousToken?v=5.274&client_id=6287487",
			bp, transport,
		)
		if err != nil {
			return "", "", "", fmt.Errorf("VK call token request: %w", err)
		}

		// Check for captcha
		if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
			errCode, _ := errObj["error_code"].(float64)
			if errCode == 14 {
				if attempt == maxCaptchaAttempts {
					return "", "", "", fmt.Errorf("captcha failed after %d attempts", maxCaptchaAttempts)
				}
				ce := parseVkCaptchaError(errObj)
				if ce.SessionToken == "" {
					return "", "", "", fmt.Errorf("image captcha not supported (no session_token)")
				}
				ctx := context.Background()
				successToken, solveErr := solveVkCaptcha(ctx, ce, bp, transport, logf)
				if solveErr != nil {
					return "", "", "", fmt.Errorf("captcha solve: %w", solveErr)
				}
				if ce.CaptchaAttempt == "0" || ce.CaptchaAttempt == "" {
					ce.CaptchaAttempt = "1"
				}
				data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s&captcha_key=&captcha_sid=%s&is_sound_captcha=0&success_token=%s&captcha_ts=%s&captcha_attempt=%s",
					link, name, token1, ce.CaptchaSid, neturl.QueryEscape(successToken), ce.CaptchaTs, ce.CaptchaAttempt)
				continue
			}
			return "", "", "", fmt.Errorf("VK API error %d: %s", int(errCode), errObj["error_msg"])
		}

		token2, err = getString(resp, "response", "token")
		if err != nil {
			return "", "", "", fmt.Errorf("VK call token parse: %w", err)
		}
		break
	}

	// Step 3: OK session
	resp, err = doHTTPPost(
		fmt.Sprintf("session_data=%%7B%%22version%%22%%3A2%%2C%%22device_id%%22%%3A%%22%s%%22%%2C%%22client_version%%22%%3A1.1%%2C%%22client_type%%22%%3A%%22SDK_JS%%22%%7D&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", uuid.New()),
		"https://calls.okcdn.ru/fb.do",
		bp, transport,
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
		bp, transport,
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
