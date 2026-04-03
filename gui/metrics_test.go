package main

import (
	"testing"
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
