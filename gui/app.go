package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/politologhse/good-turn/internal/creds"
	"github.com/politologhse/good-turn/internal/doctor"
	"github.com/politologhse/good-turn/internal/profile"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

type AppState string

const (
	StateDisconnected AppState = "disconnected"
	StateConnecting   AppState = "connecting"
	StateConnected    AppState = "connected"
	StateError        AppState = "error"
)

type ConnectConfig struct {
	VkLink      string `json:"vkLink"`
	PeerAddr    string `json:"peerAddr"`
	HyPassword  string `json:"hyPassword"`
	SNI         string `json:"sni"`
	TurnHost    string `json:"turnHost"`
	TurnPort    string `json:"turnPort"`
	UDP         bool   `json:"udp"`
	NoDTLS      bool   `json:"noDtls"`
	Streams     int    `json:"streams"`
	SocksPort   int    `json:"socksPort"`
	HTTPPort    int    `json:"httpPort"`
	SystemProxy bool   `json:"systemProxy"`
	Insecure    bool   `json:"insecure"`
}

type StatusInfo struct {
	State   AppState `json:"state"`
	Message string   `json:"message"`
	Version string   `json:"version"`
}

type App struct {
	ctx        context.Context
	wailsReady bool // true after Wails startup() callback
	mu         sync.Mutex
	state      AppState
	message    string

	relay    *Relay
	hysteria *HysteriaManager
	proxy    SystemProxy

	proxyEnabled bool
	cancel       context.CancelFunc
}

func NewApp() *App {
	return &App{
		state: StateDisconnected,
		proxy: NewSystemProxy(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.wailsReady = true
	// #8 fix: cleanup stale proxy settings on startup
	_ = a.proxy.Disable()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.teardown()
}

func (a *App) setState(state AppState, msg string) {
	a.state = state
	a.message = msg
	if a.wailsReady {
		wailsrt.EventsEmit(a.ctx, "state-change", map[string]string{
			"state":   string(state),
			"message": msg,
		})
	}
}

func (a *App) log(msg string) {
	if a.wailsReady {
		wailsrt.EventsEmit(a.ctx, "log", msg)
	}
}

// ImportProfile parses a gt:// string and returns the profile fields.
// Called from frontend instead of parsing in JavaScript.
func (a *App) ImportProfile(raw string) (map[string]string, error) {
	p, err := profile.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid profile: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("profile validation: %w", err)
	}
	return map[string]string{
		"addr":     p.Addr,
		"password": p.Password,
		"sni":      p.SNIOrDefault(),
	}, nil
}

func (a *App) GetStatus() StatusInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return StatusInfo{State: a.state, Message: a.message, Version: version}
}

// #2 fix: Connect runs heavy work in a goroutine so it doesn't block the UI.
// Mutex is only held briefly for state checks, not during network I/O.
func (a *App) Connect(cfg ConnectConfig) error {
	a.mu.Lock()
	if a.state == StateConnected || a.state == StateConnecting {
		a.mu.Unlock()
		return fmt.Errorf("already connected")
	}

	// Validate
	if cfg.PeerAddr == "" {
		a.mu.Unlock()
		return fmt.Errorf("server address is required")
	}
	if cfg.VkLink == "" {
		a.mu.Unlock()
		return fmt.Errorf("VK link is required")
	}
	if cfg.HyPassword == "" {
		a.mu.Unlock()
		return fmt.Errorf("Hysteria2 password is required")
	}
	if cfg.SocksPort == 0 {
		cfg.SocksPort = 1080
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.Streams <= 0 {
		cfg.Streams = 1
	}
	if cfg.SNI == "" {
		cfg.SNI = "hy2"
	}
	// Self-signed certs (default SNI "hy2") need insecure mode
	if cfg.SNI == "hy2" {
		cfg.Insecure = true
	}

	a.setState(StateConnecting, "Connecting...")
	a.mu.Unlock()

	// Heavy work runs without holding the mutex
	go a.connectAsync(cfg)
	return nil
}

func (a *App) connectAsync(cfg ConnectConfig) {
	// Parse link
	parts := strings.Split(cfg.VkLink, "join/")
	link := parts[len(parts)-1]
	dialer := dnsdialer.New(
		dnsdialer.WithResolvers("77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53"),
		dnsdialer.WithStrategy(dnsdialer.Fallback{}),
		dnsdialer.WithCache(100, 10*time.Hour, 10*time.Hour),
	)
	// Cache creds for 5 min, cooldown 30s between API calls, retry 3x on failure
	getCreds := creds.NewCachedCreds(
		withRetry(func(s string) (string, string, string, error) {
			return getVkCreds(s, dialer, a.log)
		}, 3, a.log),
		5*time.Minute, 30*time.Second, a.log,
	)
	if idx := strings.IndexAny(link, "/?#"); idx != -1 {
		link = link[:idx]
	}

	ctx, cancel := context.WithCancel(context.Background())

	a.mu.Lock()
	// Check if we were disconnected while setting up
	if a.state != StateConnecting {
		cancel()
		a.mu.Unlock()
		return
	}
	a.cancel = cancel
	a.mu.Unlock()

	listenAddr := "127.0.0.1:9000"

	// [1/4] Start relay (TURN + DTLS)
	a.log("[1/4] Starting TURN relay...")
	relay := NewRelay(RelayConfig{
		PeerAddr:   cfg.PeerAddr,
		ListenAddr: listenAddr,
		TurnHost:   cfg.TurnHost,
		TurnPort:   cfg.TurnPort,
		Link:       link,
		UDP:        cfg.UDP,
		NoDTLS:     cfg.NoDTLS,
		Streams:    cfg.Streams,
		GetCreds:   getCreds,
	}, a.log)

	if err := relay.Start(ctx); err != nil {
		// Check if it's a manual captcha request
		var ce *creds.CaptchaError
		if errors.As(err, &ce) && ce.Kind == creds.CaptchaManualNeeded && ce.RedirectURI != "" {
			a.log("[1/4] Manual verification required — open the link in your browser")
			if a.wailsReady {
				wailsrt.EventsEmit(a.ctx, "captcha-manual", ce.RedirectURI)
			}
			a.mu.Lock()
			a.setState(StateError, "Manual captcha verification required")
			a.mu.Unlock()
			return
		}
		cancel()
		a.mu.Lock()
		a.setState(StateError, fmt.Sprintf("[1/4] Relay failed: %s", err))
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	a.relay = relay
	a.mu.Unlock()

	// [2/4] Start Hysteria2
	a.log("[2/4] Starting Hysteria2...")
	hysteria := NewHysteriaManager(a.log)
	if err := hysteria.Start(ctx, HysteriaConfig{
		ServerAddr: listenAddr,
		Password:   cfg.HyPassword,
		SNI:        cfg.SNI,
		Insecure:   cfg.Insecure,
		SocksPort:  cfg.SocksPort,
		HTTPPort:   cfg.HTTPPort,
	}); err != nil {
		a.mu.Lock()
		a.teardown()
		a.setState(StateError, fmt.Sprintf("[2/4] Hysteria2 failed: %s", err))
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	a.hysteria = hysteria
	a.mu.Unlock()

	// [3/4] Enable system proxy
	if cfg.SystemProxy {
		a.log("[3/4] Enabling system proxy...")
		if err := a.proxy.Enable(cfg.SocksPort); err != nil {
			a.log(fmt.Sprintf("System proxy failed: %s", err))
		} else {
			a.mu.Lock()
			a.proxyEnabled = true
			a.mu.Unlock()
			a.log(fmt.Sprintf("System SOCKS5 proxy enabled on :%d", cfg.SocksPort))
		}
	}

	// [4/4] Connected
	a.log("[4/4] Connection established")
	a.mu.Lock()
	a.setState(StateConnected, fmt.Sprintf("SOCKS5 :%d | HTTP :%d", cfg.SocksPort, cfg.HTTPPort))
	a.mu.Unlock()

	// Watch for Hysteria2 crash and propagate to UI
	go a.watchHysteria(ctx)
}

// #4 fix: monitor Hysteria2 process, set error state if it dies unexpectedly
func (a *App) watchHysteria(ctx context.Context) {
	a.mu.Lock()
	hy := a.hysteria
	a.mu.Unlock()
	if hy == nil || hy.done == nil {
		return
	}

	select {
	case <-ctx.Done():
		// Normal shutdown, do nothing
		return
	case <-hy.done:
		// Hysteria2 exited unexpectedly
		a.mu.Lock()
		if a.state == StateConnected {
			a.log("Hysteria2 process died, disconnecting...")
			a.teardown()
			a.setState(StateError, "Hysteria2 crashed")
		}
		a.mu.Unlock()
	}
}

// GetMetrics returns current connection metrics (called from frontend on timer).
func (a *App) GetMetrics() MetricsSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.relay != nil {
		return a.relay.Metrics.Snapshot()
	}
	return MetricsSnapshot{}
}

// RunDoctor performs connection diagnostics without connecting.
func (a *App) RunDoctor(profileStr, vkLink string, socksPort int, noDtls bool) doctor.Report {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return doctor.Run(ctx, doctor.Config{
		ProfileString: profileStr,
		VkLink:        vkLink,
		SocksPort:     socksPort,
		NoDTLS:        noDtls,
	})
}

// CaptchaCompleted is called from frontend after user manually verified captcha.
func (a *App) CaptchaCompleted() {
	a.log("Manual captcha verification acknowledged")
}

func (a *App) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.state == StateDisconnected {
		return nil
	}

	a.teardown()
	a.setState(StateDisconnected, "")
	return nil
}

// #11 fix: cancel context FIRST, then stop subsystems
func (a *App) teardown() {
	// Signal everything to stop
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	// Disable proxy
	if a.proxyEnabled {
		if err := a.proxy.Disable(); err != nil {
			a.log(fmt.Sprintf("Disable proxy: %s", err))
		}
		a.proxyEnabled = false
	}
	// Wait for subsystems
	if a.hysteria != nil {
		a.hysteria.Stop()
		a.hysteria = nil
	}
	if a.relay != nil {
		a.relay.Stop()
		a.relay = nil
	}
}
