package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type HysteriaManager struct {
	logf       logFunc
	cancel     context.CancelFunc
	configPath string
	done       chan struct{}
}

func NewHysteriaManager(logf logFunc) *HysteriaManager {
	return &HysteriaManager{logf: logf}
}

func (h *HysteriaManager) FindBinary() (string, error) {
	name := "hysteria"
	if runtime.GOOS == "windows" {
		name = "hysteria.exe"
	}

	// Check next to our executable
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Check current directory
	if _, err := os.Stat(name); err == nil {
		abs, _ := filepath.Abs(name)
		return abs, nil
	}

	// Check PATH
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("hysteria binary not found. Place it next to the app or add to PATH")
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
	configContent := fmt.Sprintf(`server: %s

auth: %s

tls:
  sni: %s
  insecure: %v

socks5:
  listen: 127.0.0.1:%d

http:
  listen: 127.0.0.1:%d
`, cfg.ServerAddr, cfg.Password, cfg.SNI, cfg.Insecure, cfg.SocksPort, cfg.HTTPPort)

	// Write temp config with restricted permissions
	tmpFile, err := os.CreateTemp("", "hysteria-*.yaml")
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	os.Chmod(tmpFile.Name(), 0600)
	if _, err := tmpFile.WriteString(configContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return fmt.Errorf("write config: %w", err)
	}
	tmpFile.Close()
	h.configPath = tmpFile.Name()

	ctx, cancel := context.WithCancel(parentCtx)
	h.cancel = cancel
	h.done = make(chan struct{})

	cmd := exec.CommandContext(ctx, binary, "client", "-c", h.configPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		os.Remove(h.configPath)
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		os.Remove(h.configPath)
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		os.Remove(h.configPath)
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
		os.Remove(h.configPath)
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
