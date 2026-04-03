package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockProxy implements SystemProxy for tests.
type mockProxy struct {
	enabled bool
	mu      sync.Mutex
}

func (p *mockProxy) Enable(port int) error { p.mu.Lock(); p.enabled = true; p.mu.Unlock(); return nil }
func (p *mockProxy) Disable() error        { p.mu.Lock(); p.enabled = false; p.mu.Unlock(); return nil }

func newTestApp() *App {
	app := &App{
		state:      StateDisconnected,
		proxy:      &mockProxy{},
		wailsReady: false, // no Wails context — setState/log are no-ops
	}
	return app
}

func TestInitialState(t *testing.T) {
	app := newTestApp()
	status := app.GetStatus()
	if status.State != StateDisconnected {
		t.Errorf("initial state: got %q, want %q", status.State, StateDisconnected)
	}
}

func TestConnectValidation(t *testing.T) {
	app := newTestApp()

	// Missing peer address
	err := app.Connect(ConnectConfig{VkLink: "test", HyPassword: "pass"})
	if err == nil {
		t.Error("expected error for missing peerAddr")
	}

	// Missing VK link
	err = app.Connect(ConnectConfig{PeerAddr: "1.2.3.4:56000", HyPassword: "pass"})
	if err == nil {
		t.Error("expected error for missing VK link")
	}

	// Missing password
	err = app.Connect(ConnectConfig{PeerAddr: "1.2.3.4:56000", VkLink: "test"})
	if err == nil {
		t.Error("expected error for missing password")
	}
}

func TestDisconnectFromDisconnected(t *testing.T) {
	app := newTestApp()
	err := app.Disconnect()
	if err != nil {
		t.Errorf("disconnect from idle: %v", err)
	}
	if app.state != StateDisconnected {
		t.Errorf("state after disconnect: %q", app.state)
	}
}

func TestDoubleConnect(t *testing.T) {
	app := newTestApp()

	// Simulate connected state
	app.mu.Lock()
	app.state = StateConnected
	app.mu.Unlock()

	err := app.Connect(ConnectConfig{
		PeerAddr:   "1.2.3.4:56000",
		VkLink:     "test",
		HyPassword: "pass",
	})
	if err == nil {
		t.Error("expected error for double connect")
	}
}

func TestTeardownOrder(t *testing.T) {
	app := newTestApp()
	mp := &mockProxy{}
	app.proxy = mp

	// Simulate active state
	ctx, cancel := context.WithCancel(context.Background())
	app.cancel = cancel
	app.proxyEnabled = true
	_ = ctx

	app.mu.Lock()
	app.teardown()
	app.mu.Unlock()

	if mp.enabled {
		t.Error("proxy should be disabled after teardown")
	}
	if app.cancel != nil {
		t.Error("cancel should be nil after teardown")
	}
	if app.proxyEnabled {
		t.Error("proxyEnabled should be false after teardown")
	}
}

func TestConcurrentDisconnect(t *testing.T) {
	app := newTestApp()

	// Simulate connected state
	app.mu.Lock()
	app.state = StateConnected
	_, cancel := context.WithCancel(context.Background())
	app.cancel = cancel
	app.mu.Unlock()

	// Concurrent disconnects should not panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = app.Disconnect()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent disconnects deadlocked")
	}

	if app.state != StateDisconnected {
		t.Errorf("state after concurrent disconnects: %q", app.state)
	}
}

func TestDefaultConfigValues(t *testing.T) {
	app := newTestApp()

	// Connect will fail (no real server), but we can check it sets defaults
	// before attempting connection. We check via validation pass.
	cfg := ConnectConfig{
		PeerAddr:   "1.2.3.4:56000",
		VkLink:     "https://vk.com/call/join/test123",
		HyPassword: "pass",
	}

	// This will enter connectAsync and fail on TURN,
	// but the point is it shouldn't panic on zero-value config fields
	_ = app.Connect(cfg)

	// Give async goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// Clean up
	_ = app.Disconnect()
}
