package main

import (
	"sync/atomic"
	"time"
)

// ConnMetrics tracks connection health stats, exposed to the frontend.
type ConnMetrics struct {
	BytesUp       atomic.Int64
	BytesDown     atomic.Int64
	Reconnects    atomic.Int64
	ConnectedAt   atomic.Int64 // unix timestamp
	LastActivityAt atomic.Int64 // unix timestamp
}

func (m *ConnMetrics) RecordUp(n int) {
	m.BytesUp.Add(int64(n))
	m.LastActivityAt.Store(time.Now().Unix())
}

func (m *ConnMetrics) RecordDown(n int) {
	m.BytesDown.Add(int64(n))
	m.LastActivityAt.Store(time.Now().Unix())
}

func (m *ConnMetrics) RecordReconnect() {
	m.Reconnects.Add(1)
}

func (m *ConnMetrics) MarkConnected() {
	m.ConnectedAt.Store(time.Now().Unix())
	m.LastActivityAt.Store(time.Now().Unix())
}

type MetricsSnapshot struct {
	BytesUp    int64  `json:"bytesUp"`
	BytesDown  int64  `json:"bytesDown"`
	Reconnects int64  `json:"reconnects"`
	UptimeSec  int64  `json:"uptimeSec"`
	IdleSec    int64  `json:"idleSec"`
}

func (m *ConnMetrics) Snapshot() MetricsSnapshot {
	now := time.Now().Unix()
	connAt := m.ConnectedAt.Load()
	lastAct := m.LastActivityAt.Load()
	var uptime, idle int64
	if connAt > 0 {
		uptime = now - connAt
	}
	if lastAct > 0 {
		idle = now - lastAct
	}
	return MetricsSnapshot{
		BytesUp:    m.BytesUp.Load(),
		BytesDown:  m.BytesDown.Load(),
		Reconnects: m.Reconnects.Load(),
		UptimeSec:  uptime,
		IdleSec:    idle,
	}
}
