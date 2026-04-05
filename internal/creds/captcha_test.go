package creds

import (
	"errors"
	"testing"
)

func TestCaptchaErrorKinds(t *testing.T) {
	auto := &CaptchaError{Kind: CaptchaAutoFailed, Message: "PoW exhausted"}
	manual := &CaptchaError{Kind: CaptchaManualNeeded, Message: "BOT detected", RedirectURI: "https://vk.ru/captcha"}
	unsupported := &CaptchaError{Kind: CaptchaUnsupported, Message: "image captcha"}
	network := &CaptchaError{Kind: CaptchaNetworkError, Message: "timeout"}

	if auto.Kind != CaptchaAutoFailed {
		t.Error("auto kind mismatch")
	}
	if manual.RedirectURI == "" {
		t.Error("manual should have redirect URI")
	}
	if unsupported.Kind != CaptchaUnsupported {
		t.Error("unsupported kind mismatch")
	}
	if network.Error() != "timeout" {
		t.Errorf("error message: %q", network.Error())
	}
}

func TestCaptchaErrorAs(t *testing.T) {
	err := &CaptchaError{Kind: CaptchaManualNeeded, Message: "BOT", RedirectURI: "https://vk.ru/test"}
	var ce *CaptchaError
	if !errors.As(err, &ce) {
		t.Fatal("errors.As should match CaptchaError")
	}
	if ce.Kind != CaptchaManualNeeded {
		t.Errorf("kind: %d", ce.Kind)
	}
	if ce.RedirectURI != "https://vk.ru/test" {
		t.Errorf("uri: %q", ce.RedirectURI)
	}
}

func TestParseVkCaptchaError(t *testing.T) {
	errData := map[string]interface{}{
		"error_code":   float64(14),
		"error_msg":    "Captcha needed",
		"captcha_sid":  float64(123456789),
		"redirect_uri": "https://vk.ru/captcha?sid=123&session_token=abc123",
		"captcha_ts":   float64(1700000000),
	}

	ce := parseVkCaptchaError(errData)
	if ce.ErrorCode != 14 {
		t.Errorf("error code: %d", ce.ErrorCode)
	}
	if ce.CaptchaSid != "123456789" {
		t.Errorf("captcha_sid: %q", ce.CaptchaSid)
	}
	if ce.SessionToken != "abc123" {
		t.Errorf("session_token: %q", ce.SessionToken)
	}
	if ce.CaptchaTs != "1700000000" {
		t.Errorf("captcha_ts: %q", ce.CaptchaTs)
	}
}

func TestParseVkCaptchaErrorStringSid(t *testing.T) {
	errData := map[string]interface{}{
		"error_code":  float64(14),
		"captcha_sid": "string-sid-value",
	}
	ce := parseVkCaptchaError(errData)
	if ce.CaptchaSid != "string-sid-value" {
		t.Errorf("string sid: %q", ce.CaptchaSid)
	}
}

func TestBrowserProfileApplyHeaders(t *testing.T) {
	bp := RandomProfile()
	if bp.UserAgent == "" {
		t.Error("empty user agent")
	}
	if bp.SecCHUA == "" {
		t.Error("empty Sec-CH-UA")
	}
	if bp.SecPlatform == "" {
		t.Error("empty Sec-CH-UA-Platform")
	}
}
