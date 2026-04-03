package main

import (
	"fmt"
	"testing"
	"time"
)

func TestMetricsRecording(t *testing.T) {
	var m ConnMetrics

	m.MarkConnected()
	m.RecordUp(100)
	m.RecordUp(200)
	m.RecordDown(500)
	m.RecordReconnect()
	m.RecordReconnect()

	snap := m.Snapshot()

	if snap.BytesUp != 300 {
		t.Errorf("BytesUp: got %d, want 300", snap.BytesUp)
	}
	if snap.BytesDown != 500 {
		t.Errorf("BytesDown: got %d, want 500", snap.BytesDown)
	}
	if snap.Reconnects != 2 {
		t.Errorf("Reconnects: got %d, want 2", snap.Reconnects)
	}
	if snap.UptimeSec < 0 {
		t.Errorf("UptimeSec negative: %d", snap.UptimeSec)
	}
}

func TestMetricsZeroBeforeConnect(t *testing.T) {
	var m ConnMetrics
	snap := m.Snapshot()
	if snap.BytesUp != 0 || snap.BytesDown != 0 || snap.UptimeSec != 0 {
		t.Errorf("expected zeroes, got %+v", snap)
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

	wrapped := withRetry(f, 3, logf)
	user, pass, addr, err := wrapped("test")

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if user != "user" || pass != "pass" || addr != "addr" {
		t.Errorf("got %q %q %q", user, pass, addr)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 retry logs, got %d", len(logs))
	}
}

func TestWithRetryAllFail(t *testing.T) {
	f := func(link string) (string, string, string, error) {
		return "", "", "", fmt.Errorf("always fails")
	}
	logf := func(msg string) {}

	wrapped := withRetry(f, 2, logf)

	start := time.Now()
	_, _, _, err := wrapped("test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed < 1*time.Second {
		t.Errorf("expected backoff delay, elapsed %s", elapsed)
	}
}
