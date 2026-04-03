package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey           = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

var (
	wininet            = syscall.NewLazyDLL("wininet.dll")
	internetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

type windowsProxy struct{}

func NewSystemProxy() SystemProxy {
	return &windowsProxy{}
}

func (p *windowsProxy) Enable(socksPort int) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue("ProxyServer", fmt.Sprintf("socks=127.0.0.1:%d", socksPort)); err != nil {
		return fmt.Errorf("set ProxyServer: %w", err)
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("set ProxyEnable: %w", err)
	}

	notifySystem()
	return nil
}

func (p *windowsProxy) Disable() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("set ProxyEnable: %w", err)
	}

	notifySystem()
	return nil
}

func notifySystem() {
	internetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	internetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
	// Suppress "unused" for unsafe — needed for potential future pointer args
	_ = unsafe.Pointer(nil)
}
