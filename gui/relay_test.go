package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/politologhse/good-turn/internal/creds"
)

func TestNoDTLS_CaptchaManualPropagates(t *testing.T) {
	// GetCreds always returns CaptchaManualNeeded
	getCreds := func(link string) (string, string, string, error) {
		return "", "", "", &creds.CaptchaError{
			Kind:        creds.CaptchaManualNeeded,
			Message:     "BOT detected",
			RedirectURI: "https://vk.ru/captcha?test=1",
		}
	}

	relay := NewRelay(RelayConfig{
		PeerAddr:   "127.0.0.1:59999",
		ListenAddr: "127.0.0.1:0", // random port
		Link:       "test-link",
		NoDTLS:     true,
		Streams:    1,
		GetCreds:   getCreds,
	}, func(string) {})

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	err := relay.Start(ctx)
	if err == nil {
		relay.Stop()
		t.Fatal("expected error, got nil")
	}

	var ce *creds.CaptchaError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CaptchaError, got: %T: %s", err, err)
	}
	if ce.Kind != creds.CaptchaManualNeeded {
		t.Errorf("expected ManualNeeded, got kind=%d", ce.Kind)
	}
	if ce.RedirectURI == "" {
		t.Error("expected RedirectURI to be set")
	}
}
