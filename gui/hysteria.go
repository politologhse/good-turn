package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/politologhse/good-turn/internal/hybin"
)

type HysteriaManager struct {
	logf       logFunc
	cancel     context.CancelFunc
	configPath string
	done       chan struct{}
}

func NewHysteriaManager(logf logFunc) *HysteriaManager {
	return &HysteriaManager{logf: logf, done: make(chan struct{})}
}

func (h *HysteriaManager) FindBinary() (string, error) {
	return hybin.Find()
}

type HysteriaConfig struct {
	ServerAddr string
	Password   string
	SNI        string
	Insecure   bool
	SocksPort  int
	HTTPPort   int
}

func (h *HysteriaManager) Start(parentCtx context.Context, cfg HysteriaConfig) error {
	binary, err := h.FindBinary()
	if err != nil {
		return err
	}

	// Generate config YAML
	// ACL: RU domains/IPs go direct (no tunnel), everything else proxied
	configContent := fmt.Sprintf(`server: %s

auth: %s

tls:
  sni: %s
  insecure: %v

socks5:
  listen: 127.0.0.1:%d

http:
  listen: 127.0.0.1:%d

acl:
  inline:
    - direct(geoip:private)
    - direct(geosite:category-ru)
    - direct(geoip:ru)
    - proxy(all)
`, cfg.ServerAddr, cfg.Password, cfg.SNI, cfg.Insecure, cfg.SocksPort, cfg.HTTPPort)

	// Write temp config with restricted permissions
	tmpFile, err := os.CreateTemp("", "hysteria-*.yaml")
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	_ = os.Chmod(tmpFile.Name(), 0600)
	if _, err := tmpFile.WriteString(configContent); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return fmt.Errorf("write config: %w", err)
	}
	_ = tmpFile.Close()
	h.configPath = tmpFile.Name()

	ctx, cancel := context.WithCancel(parentCtx)
	h.cancel = cancel
	h.done = make(chan struct{}) // reset for new run

	cmd := exec.CommandContext(ctx, binary, "client", "-c", h.configPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(h.configPath)
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = os.Remove(h.configPath)
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(h.configPath)
		return fmt.Errorf("start hysteria: %w", err)
	}

	h.logf(fmt.Sprintf("Hysteria2 started (PID %d)", cmd.Process.Pid))

	// Stream output
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				h.logf("[hy2] " + line)
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				h.logf("[hy2] " + line)
			}
		}
	}()

	// Wait for exit in background
	go func() {
		defer close(h.done)
		if err := cmd.Wait(); err != nil {
			if ctx.Err() == nil {
				h.logf(fmt.Sprintf("Hysteria2 exited: %s", err))
			}
		}
		_ = os.Remove(h.configPath)
		h.configPath = ""
	}()

	return nil
}

func (h *HysteriaManager) Stop() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	if h.done != nil {
		<-h.done
		h.done = nil
	}
	h.logf("Hysteria2 stopped")
}
