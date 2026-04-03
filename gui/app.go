package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
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
	YandexLink  string `json:"yandexLink"`
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
}

type App struct {
	ctx     context.Context
	mu      sync.Mutex
	state   AppState
	message string

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
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.teardown()
}

func (a *App) setState(state AppState, msg string) {
	a.state = state
	a.message = msg
	wailsrt.EventsEmit(a.ctx, "state-change", map[string]string{
		"state":   string(state),
		"message": msg,
	})
}

func (a *App) log(msg string) {
	wailsrt.EventsEmit(a.ctx, "log", msg)
}

func (a *App) GetStatus() StatusInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return StatusInfo{State: a.state, Message: a.message}
}

func (a *App) Connect(cfg ConnectConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.state == StateConnected || a.state == StateConnecting {
		return fmt.Errorf("already connected")
	}

	// Validate
	if cfg.PeerAddr == "" {
		return fmt.Errorf("server address is required")
	}
	if cfg.VkLink == "" && cfg.YandexLink == "" {
		return fmt.Errorf("VK or Yandex link is required")
	}
	if cfg.HyPassword == "" {
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
		cfg.Insecure = true
	}

	a.setState(StateConnecting, "Connecting...")

	// Parse link
	var link string
	var getCreds getCredsFunc
	if cfg.VkLink != "" {
		parts := strings.Split(cfg.VkLink, "join/")
		link = parts[len(parts)-1]
		dialer := dnsdialer.New(
			dnsdialer.WithResolvers("77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53"),
			dnsdialer.WithStrategy(dnsdialer.Fallback{}),
			dnsdialer.WithCache(100, 10*time.Hour, 10*time.Hour),
		)
		getCreds = func(s string) (string, string, string, error) {
			return getVkCreds(s, dialer, a.log)
		}
	} else {
		parts := strings.Split(cfg.YandexLink, "j/")
		link = parts[len(parts)-1]
		getCreds = func(s string) (string, string, string, error) {
			return getYandexCreds(s, a.log)
		}
	}
	if idx := strings.IndexAny(link, "/?#"); idx != -1 {
		link = link[:idx]
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	listenAddr := "127.0.0.1:9000"

	// 1. Start relay
	a.relay = NewRelay(RelayConfig{
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

	if err := a.relay.Start(ctx); err != nil {
		a.teardown()
		a.setState(StateError, err.Error())
		return err
	}

	// 2. Start Hysteria2
	a.hysteria = NewHysteriaManager(a.log)
	if err := a.hysteria.Start(ctx, HysteriaConfig{
		ServerAddr: listenAddr,
		Password:   cfg.HyPassword,
		SNI:        cfg.SNI,
		Insecure:   cfg.Insecure,
		SocksPort:  cfg.SocksPort,
		HTTPPort:   cfg.HTTPPort,
	}); err != nil {
		a.teardown()
		a.setState(StateError, err.Error())
		return err
	}

	// 3. Enable system proxy
	if cfg.SystemProxy {
		if err := a.proxy.Enable(cfg.SocksPort); err != nil {
			a.log(fmt.Sprintf("System proxy failed: %s", err))
		} else {
			a.proxyEnabled = true
			a.log(fmt.Sprintf("System SOCKS5 proxy enabled on :%d", cfg.SocksPort))
		}
	}

	a.setState(StateConnected, fmt.Sprintf("SOCKS5 127.0.0.1:%d | HTTP 127.0.0.1:%d", cfg.SocksPort, cfg.HTTPPort))
	return nil
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

func (a *App) teardown() {
	if a.proxyEnabled {
		if err := a.proxy.Disable(); err != nil {
			a.log(fmt.Sprintf("Disable proxy: %s", err))
		}
		a.proxyEnabled = false
	}
	if a.hysteria != nil {
		a.hysteria.Stop()
		a.hysteria = nil
	}
	if a.relay != nil {
		a.relay.Stop()
		a.relay = nil
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}
