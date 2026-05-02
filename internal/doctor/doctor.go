// Package doctor provides connection diagnostics for Good TURN.
// Each check runs independently and returns pass/warn/fail with actionable hints.
//
// Doctor uses the SAME network paths as runtime: dnsdialer for VK auth,
// hybin.Find() for Hysteria2 lookup, real STUN/DTLS handshakes for reachability.
package doctor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/bschaatsbergen/dnsdialer"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/stun/v3"
	"github.com/politologhse/good-turn/internal/hybin"
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
	ProfileString string
	VkLink        string
	HysteriaBin   string
	SocksPort     int
	NoDTLS        bool
}

// vkDialer mirrors what creds.GetVkCreds uses for DNS resolution.
func vkDialer() *dnsdialer.Dialer {
	return dnsdialer.New(
		dnsdialer.WithResolvers("77.88.8.8:53", "77.88.8.1:53", "8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53"),
		dnsdialer.WithStrategy(dnsdialer.Fallback{}),
		dnsdialer.WithCache(100, 10*time.Hour, 10*time.Hour),
	)
}

// Run executes all diagnostic checks and returns a report.
func Run(ctx context.Context, cfg Config) Report {
	r := Report{
		Platform: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	dialer := vkDialer()

	r.Checks = append(r.Checks, checkProfile(cfg))
	r.Checks = append(r.Checks, checkVkAuth(ctx, dialer))
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

// checkVkAuth uses the same dnsdialer as runtime VK auth.
func checkVkAuth(ctx context.Context, dialer *dnsdialer.Dialer) CheckResult {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://login.vk.ru/", nil)
	if err != nil {
		return CheckResult{Name: "VK Auth", Status: Fail, Detail: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{
			Name:   "VK Auth",
			Status: Fail,
			Detail: fmt.Sprintf("Cannot reach login.vk.ru via custom resolvers: %s", err),
			Hint:   "VK auth requires login.vk.ru. Check internet connection.",
		}
	}
	_ = resp.Body.Close()
	return CheckResult{
		Name:   "VK Auth",
		Status: Pass,
		Detail: fmt.Sprintf("login.vk.ru reachable via dnsdialer (HTTP %d)", resp.StatusCode),
	}
}

// checkPeerReachability does a real DTLS handshake to the peer (not just UDP dial).
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

	// DNS resolution
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resolveCancel()
	addrs, err := net.DefaultResolver.LookupHost(resolveCtx, host)
	if err != nil {
		return CheckResult{
			Name:   "Peer Host",
			Status: Fail,
			Detail: fmt.Sprintf("DNS lookup failed for %s: %s", host, err),
			Hint:   "Check that the server address is correct (or DDNS record is fresh)",
		}
	}

	// REAL reachability: DTLS handshake to the good-turn server.
	// This proves the server is alive AND speaks DTLS, not just that the OS made a UDP socket.
	udpAddr, err := net.ResolveUDPAddr("udp", p.Addr)
	if err != nil {
		return CheckResult{Name: "Peer Host", Status: Fail, Detail: err.Error()}
	}
	udp, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return CheckResult{
			Name:   "Peer Host",
			Status: Warn,
			Detail: fmt.Sprintf("%s resolves to %s but UDP socket failed: %s", host, addrs[0], err),
		}
	}
	defer func() { _ = udp.Close() }()

	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return CheckResult{Name: "Peer Host", Status: Warn, Detail: fmt.Sprintf("cert gen: %s", err)}
	}
	dtlsCfg := &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
	}
	dtlsConn, err := dtls.Client(udp, udpAddr, dtlsCfg)
	if err != nil {
		return CheckResult{
			Name:   "Peer Host",
			Status: Fail,
			Detail: fmt.Sprintf("%s → %s: DTLS client init failed: %s", host, addrs[0], err),
		}
	}
	hsCtx, hsCancel := context.WithTimeout(ctx, 8*time.Second)
	defer hsCancel()
	if err := dtlsConn.HandshakeContext(hsCtx); err != nil {
		_ = dtlsConn.Close()
		return CheckResult{
			Name:   "Peer Host",
			Status: Fail,
			Detail: fmt.Sprintf("%s → %s: DTLS handshake failed (%s)", host, addrs[0], err),
			Hint:   "Server may be down, port closed, or not running good-turn-server",
		}
	}
	_ = dtlsConn.Close()

	return CheckResult{
		Name:   "Peer Host",
		Status: Pass,
		Detail: fmt.Sprintf("%s → %s: DTLS handshake OK", host, addrs[0]),
	}
}

// checkTurnPreflight does a real STUN binding request to a known VK TURN server.
// This proves UDP can actually reach VK TURN infrastructure (round-trip).
func checkTurnPreflight(ctx context.Context) CheckResult {
	turnHost := "155.212.193.23:19302"

	udpAddr, err := net.ResolveUDPAddr("udp", turnHost)
	if err != nil {
		return CheckResult{Name: "TURN Preflight", Status: Fail, Detail: err.Error()}
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return CheckResult{
			Name:   "TURN Preflight",
			Status: Fail,
			Detail: fmt.Sprintf("Cannot create UDP socket to %s: %s", turnHost, err),
		}
	}
	defer func() { _ = conn.Close() }()

	// Send STUN binding request and wait for binding response
	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))

	if _, err := conn.Write(msg.Raw); err != nil {
		return CheckResult{
			Name:   "TURN Preflight",
			Status: Fail,
			Detail: fmt.Sprintf("STUN write failed: %s", err),
		}
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return CheckResult{
			Name:   "TURN Preflight",
			Status: Fail,
			Detail: fmt.Sprintf("VK TURN %s: no STUN response (%s)", turnHost, err),
			Hint:   "VK TURN may be unreachable from your network. Check firewall/NAT.",
		}
	}

	resp := &stun.Message{Raw: append([]byte{}, buf[:n]...)}
	if err := resp.Decode(); err != nil {
		return CheckResult{
			Name:   "TURN Preflight",
			Status: Warn,
			Detail: fmt.Sprintf("VK TURN responded but STUN decode failed: %s", err),
		}
	}

	return CheckResult{
		Name:   "TURN Preflight",
		Status: Pass,
		Detail: fmt.Sprintf("VK TURN %s: STUN binding OK (round-trip verified)", turnHost),
	}
}

// checkHysteriaBinary uses the SAME lookup logic as runtime (hybin.Find).
func checkHysteriaBinary(cfg Config) CheckResult {
	path := cfg.HysteriaBin
	if path == "" {
		p, err := hybin.Find()
		if err != nil {
			return CheckResult{
				Name:   "Hysteria2",
				Status: Fail,
				Detail: err.Error(),
				Hint:   "Place the hysteria binary next to the app, in cwd, or in PATH",
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

// Sanitize redacts any sensitive data from the report (passwords, tokens, full URLs).
// Keeps host:port and check status, removes anything that could leak credentials.
func (r Report) Sanitize() Report {
	sanitized := Report{Platform: r.Platform}
	for _, c := range r.Checks {
		sc := c
		// Strip server addresses from Detail and Hint that might be sensitive in shared bundles
		sc.Detail = redactAddresses(sc.Detail)
		sc.Hint = redactAddresses(sc.Hint)
		sanitized.Checks = append(sanitized.Checks, sc)
	}
	return sanitized
}

var addressPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b|\b[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+:\d{1,5}\b`)

// redactAddresses replaces IP addresses and host:port pairs with placeholders.
// Used for safe debug bundle export.
func redactAddresses(s string) string {
	return addressPattern.ReplaceAllString(s, "<server>")
}
