package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type darwinProxy struct {
	service string
}

func NewSystemProxy() SystemProxy {
	return &darwinProxy{}
}

func (p *darwinProxy) detectService() (string, error) {
	if p.service != "" {
		return p.service, nil
	}

	// Get default route interface
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("route get default: %w", err)
	}

	var iface string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			iface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
			break
		}
	}
	if iface == "" {
		return "", fmt.Errorf("could not detect default interface")
	}

	// Map interface to network service name
	out, err = exec.Command("networksetup", "-listnetworkserviceorder").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("listnetworkserviceorder: %w", err)
	}

	// Parse output: lines like "(1) Wi-Fi" followed by "(Hardware Port: Wi-Fi, Device: en0)"
	lines := strings.Split(string(out), "\n")
	re := regexp.MustCompile(`\(\d+\)\s+(.+)`)
	reDevice := regexp.MustCompile(`Device:\s*(\S+)\)`)

	for i, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		serviceName := strings.TrimSpace(m[1])
		// Check next line for device
		if i+1 < len(lines) {
			dm := reDevice.FindStringSubmatch(lines[i+1])
			if dm != nil && dm[1] == iface {
				p.service = serviceName
				return serviceName, nil
			}
		}
	}

	// Fallback to "Wi-Fi"
	p.service = "Wi-Fi"
	return p.service, nil
}

func (p *darwinProxy) Enable(socksPort int) error {
	service, err := p.detectService()
	if err != nil {
		return err
	}

	if out, err := exec.Command("networksetup", "-setsocksfirewallproxy", service, "127.0.0.1", fmt.Sprintf("%d", socksPort)).CombinedOutput(); err != nil {
		return fmt.Errorf("set socks proxy: %s: %w", string(out), err)
	}

	if out, err := exec.Command("networksetup", "-setsocksfirewallproxystate", service, "on").CombinedOutput(); err != nil {
		return fmt.Errorf("enable socks proxy: %s: %w", string(out), err)
	}

	return nil
}

func (p *darwinProxy) Disable() error {
	service, err := p.detectService()
	if err != nil {
		return err
	}

	if out, err := exec.Command("networksetup", "-setsocksfirewallproxystate", service, "off").CombinedOutput(); err != nil {
		return fmt.Errorf("disable socks proxy: %s: %w", string(out), err)
	}

	return nil
}
