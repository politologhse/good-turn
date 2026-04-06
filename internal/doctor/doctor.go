// Package doctor provides connection diagnostics for Good TURN.
// Each check runs independently and returns pass/warn/fail with actionable hints.
package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/politologhse/good-turn/internal/profile"
)

// Status represents the result of a diagnostic check.
type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

// CheckResult is the outcome of a single diagnostic check.
type CheckResult struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

// Report is the full doctor output.
type Report struct {
	Platform string        `json:"platform"`
	Checks   []CheckResult `json:"checks"`
}

// Config holds parameters for running diagnostics.
type Config struct {
	ProfileString string // gt:// config string (optional)
	VkLink        string // VK call link (optional)
	HysteriaBin   string // path to hysteria binary (optional, auto-detected)
	SocksPort     int
	NoDTLS        bool
}

// Run executes all diagnostic checks and returns a report.
func Run(ctx context.Context, cfg Config) Report {
	r := Report{
		Platform: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	r.Checks = append(r.Checks, checkProfile(cfg))
	r.Checks = append(r.Checks, checkVkAuth(ctx))
	r.Checks = append(r.Checks, checkPeerReachability(ctx, cfg))
	r.Checks = append(r.Checks, checkTurnPreflight(ctx))
	r.Checks = append(r.Checks, checkHysteriaBinary(cfg))
	r.Checks = append(r.Checks, checkSocksPort(cfg))
	r.Checks = append(r.Checks, checkNoDTLS(cfg))

	return r
}

func checkProfile(cfg Config) CheckResult {
	if cfg.ProfileString == "" {
		return CheckResult{
			Name:   "Profile",
			Status: Warn,
			Detail: "No profile string provided",
			Hint:   "Import a gt:// config string in the app",
		}
	}
	p, err := profile.Parse(cfg.ProfileString)
	if err != nil {
		return CheckResult{
			Name:   "Profile",
			Status: Fail,
			Detail: fmt.Sprintf("Invalid profile: %s", err),
			Hint:   "Check that the gt:// string is complete and not truncated",
		}
	}
	if err := p.Validate(); err != nil {
		return CheckResult{
			Name:   "Profile",
			Status: Fail,
			Detail: fmt.Sprintf("Profile validation: %s", err),
			Hint:   "Server address must be host:port, password must not be empty",
		}
	}
	return CheckResult{
		Name:   "Profile",
		Status: Pass,
		Detail: fmt.Sprintf("Valid profile → %s (SNI: %s)", p.Addr, p.SNIOrDefault()),
	}
}

func checkVkAuth(ctx context.Context) CheckResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://login.vk.ru/", nil)
	if err != nil {
		return CheckResult{Name: "VK Auth", Status: Fail, Detail: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CheckResult{
			Name:   "VK Auth",
			Status: Fail,
			Detail: fmt.Sprintf("Cannot reach login.vk.ru: %s", err),
			Hint:   "Check internet connection. VK auth requires access to login.vk.ru",
		}
	}
	_ = resp.Body.Close()
	return CheckResult{
		Name:   "VK Auth",
		Status: Pass,
		Detail: fmt.Sprintf("login.vk.ru reachable (HTTP %d)", resp.StatusCode),
	}
}

func checkPeerReachability(ctx context.Context, cfg Config) CheckResult {
	if cfg.ProfileString == "" {
		return CheckResult{Name: "Peer Host", Status: Warn, Detail: "No profile — skipping"}
	}
	p, err := profile.Parse(cfg.ProfileString)
	if err != nil {
		return CheckResult{Name: "Peer Host", Status: Fail, Detail: err.Error()}
	}

	host, _, err := net.SplitHostPort(p.Addr)
	if err != nil {
		return CheckResult{Name: "Peer Host", Status: Fail, Detail: err.Error()}
	}

	// DNS resolution check
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return CheckResult{
			Name:   "Peer Host",
			Status: Fail,
			Detail: fmt.Sprintf("DNS lookup failed for %s: %s", host, err),
			Hint:   "Check that the server address is correct. If using a domain, verify DNS records.",
		}
	}

	// UDP reachability (quick probe to the TURN port)
	conn, err := net.DialTimeout("udp", p.Addr, 3*time.Second)
	if err != nil {
		return CheckResult{
			Name:   "Peer Host",
			Status: Warn,
			Detail: fmt.Sprintf("%s resolves to %s but UDP connect failed: %s", host, addrs[0], err),
			Hint:   "Server may be down or firewall blocking UDP. Check that port is open.",
		}
	}
	_ = conn.Close()

	return CheckResult{
		Name:   "Peer Host",
		Status: Pass,
		Detail: fmt.Sprintf("%s → %s (UDP reachable)", host, addrs[0]),
	}
}

func checkTurnPreflight(ctx context.Context) CheckResult {
	// Check if common VK TURN server IPs are reachable
	turnHost := "155.212.193.23"
	conn, err := net.DialTimeout("udp", turnHost+":19302", 3*time.Second)
	if err != nil {
		return CheckResult{
			Name:   "TURN Preflight",
			Status: Warn,
			Detail: fmt.Sprintf("Cannot reach VK TURN server %s:19302: %s", turnHost, err),
			Hint:   "VK TURN servers may be temporarily unreachable. Try again later.",
		}
	}
	_ = conn.Close()
	return CheckResult{
		Name:   "TURN Preflight",
		Status: Pass,
		Detail: fmt.Sprintf("VK TURN %s:19302 reachable", turnHost),
	}
}

func checkHysteriaBinary(cfg Config) CheckResult {
	name := "hysteria"
	if runtime.GOOS == "windows" {
		name = "hysteria.exe"
	}

	path := cfg.HysteriaBin
	if path == "" {
		p, err := exec.LookPath(name)
		if err != nil {
			return CheckResult{
				Name:   "Hysteria2",
				Status: Fail,
				Detail: "Hysteria2 binary not found",
				Hint:   "Place the hysteria binary next to the app or add it to PATH",
			}
		}
		path = p
	}

	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:   "Hysteria2",
			Status: Warn,
			Detail: fmt.Sprintf("Found at %s but version check failed: %s", path, err),
		}
	}

	version := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	return CheckResult{
		Name:   "Hysteria2",
		Status: Pass,
		Detail: fmt.Sprintf("%s (%s)", path, version),
	}
}

func checkSocksPort(cfg Config) CheckResult {
	port := cfg.SocksPort
	if port == 0 {
		port = 1080
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return CheckResult{
			Name:   "SOCKS5 Port",
			Status: Warn,
			Detail: fmt.Sprintf("Port %d already in use", port),
			Hint:   fmt.Sprintf("Change SOCKS5 port in Settings or stop the process using port %d", port),
		}
	}
	_ = ln.Close()
	return CheckResult{
		Name:   "SOCKS5 Port",
		Status: Pass,
		Detail: fmt.Sprintf("Port %d available", port),
	}
}

func checkNoDTLS(cfg Config) CheckResult {
	if cfg.NoDTLS {
		return CheckResult{
			Name:   "DTLS Mode",
			Status: Warn,
			Detail: "DTLS obfuscation is disabled",
			Hint:   "Without DTLS, traffic is not obfuscated and may be detected by DPI",
		}
	}
	return CheckResult{
		Name:   "DTLS Mode",
		Status: Pass,
		Detail: "DTLS obfuscation enabled",
	}
}

// Sanitize removes secrets from a report for safe export.
func (r Report) Sanitize() Report {
	sanitized := Report{Platform: r.Platform}
	for _, c := range r.Checks {
		sc := c
		// Redact any profile details that might contain server address
		// (addr is ok to show, password is never in check output)
		sanitized.Checks = append(sanitized.Checks, sc)
	}
	return sanitized
}
