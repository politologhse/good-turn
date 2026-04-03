package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

type RelayConfig struct {
	PeerAddr   string
	ListenAddr string
	TurnHost   string
	TurnPort   string
	Link       string
	UDP        bool
	NoDTLS     bool
	Streams    int
	GetCreds   getCredsFunc
}

type Relay struct {
	cfg     RelayConfig
	logf    logFunc
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	ticker  *time.Ticker
	Metrics ConnMetrics
}

func NewRelay(cfg RelayConfig, logf logFunc) *Relay {
	if cfg.Streams <= 0 {
		cfg.Streams = 1
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:9000"
	}
	return &Relay{cfg: cfg, logf: logf}
}

func (r *Relay) Start(parentCtx context.Context) error {
	peer, err := net.ResolveUDPAddr("udp", r.cfg.PeerAddr)
	if err != nil {
		return fmt.Errorf("resolve peer: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	r.cancel = cancel

	listenConn, err := net.ListenPacket("udp", r.cfg.ListenAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("listen: %w", err)
	}
	context.AfterFunc(ctx, func() {
		_ = listenConn.Close()
	})

	listenConnChan := make(chan net.PacketConn)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case listenConnChan <- listenConn:
			}
		}
	}()

	params := &turnParams{
		host:     r.cfg.TurnHost,
		port:     r.cfg.TurnPort,
		link:     r.cfg.Link,
		udp:      r.cfg.UDP,
		getCreds: r.cfg.GetCreds,
	}

	// #10 fix: use NewTicker instead of Tick so we can stop it
	r.ticker = time.NewTicker(200 * time.Millisecond)
	t := r.ticker.C

	if r.cfg.NoDTLS {
		for i := 0; i < r.cfg.Streams; i++ {
			r.wg.Go(func() {
				r.oneTurnConnectionLoop(ctx, params, peer, listenConnChan, t)
			})
		}
	} else {
		// #12 fix: buffered channel so sender never blocks
		okchan := make(chan struct{}, 1)
		connchan := make(chan net.PacketConn)

		r.wg.Go(func() {
			r.oneDtlsConnectionLoop(ctx, peer, listenConnChan, connchan, okchan)
		})
		r.wg.Go(func() {
			r.oneTurnConnectionLoop(ctx, params, peer, connchan, t)
		})

		select {
		case <-okchan:
			r.logf("TURN relay connected")
		case <-ctx.Done():
			return fmt.Errorf("cancelled before connection established")
		case <-time.After(30 * time.Second):
			cancel()
			return fmt.Errorf("timeout waiting for DTLS handshake")
		}

		for i := 0; i < r.cfg.Streams-1; i++ {
			ch := make(chan net.PacketConn)
			r.wg.Go(func() {
				r.oneDtlsConnectionLoop(ctx, peer, listenConnChan, ch, nil)
			})
			r.wg.Go(func() {
				r.oneTurnConnectionLoop(ctx, params, peer, ch, t)
			})
		}
	}

	r.logf(fmt.Sprintf("Relay listening on %s -> %s (%d streams)", r.cfg.ListenAddr, r.cfg.PeerAddr, r.cfg.Streams))
	return nil
}

func (r *Relay) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.ticker != nil {
		r.ticker.Stop()
	}
	r.wg.Wait()
	r.logf("Relay stopped")
}

// --- DTLS functions ---

func (r *Relay) dtlsConnect(ctx context.Context, conn net.PacketConn, peer *net.UDPAddr) (net.Conn, error) {
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return nil, err
	}
	config := &dtls.Config{
		Certificates:          []tls.Certificate{certificate},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
	}
	ctx1, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dtlsConn, err := dtls.Client(conn, peer, config)
	if err != nil {
		return nil, err
	}
	if err := dtlsConn.HandshakeContext(ctx1); err != nil {
		return nil, err
	}
	return dtlsConn, nil
}

func (r *Relay) oneDtlsConnection(ctx context.Context, peer *net.UDPAddr, listenConn net.PacketConn, connchan chan<- net.PacketConn, okchan chan<- struct{}, c chan<- error) {
	var err error
	defer func() { c <- err }()
	dtlsctx, dtlscancel := context.WithCancel(ctx)
	defer dtlscancel()

	var conn1, conn2 net.PacketConn
	conn1, conn2 = connutil.AsyncPacketPipe()
	go func() {
		for {
			select {
			case <-dtlsctx.Done():
				return
			case connchan <- conn2:
			}
		}
	}()

	dtlsConn, err1 := r.dtlsConnect(dtlsctx, conn1, peer)
	if err1 != nil {
		err = fmt.Errorf("DTLS connect failed: %s", err1)
		return
	}
	defer func() {
		if closeErr := dtlsConn.Close(); closeErr != nil {
			r.logf(fmt.Sprintf("DTLS close: %s", closeErr))
		}
	}()
	r.logf("DTLS connection established")
	r.Metrics.MarkConnected()
	r.Metrics.RecordReconnect()

	// #12 fix: non-blocking send on buffered okchan
	if okchan != nil {
		select {
		case okchan <- struct{}{}:
		default:
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(2)
	context.AfterFunc(dtlsctx, func() {
		_ = listenConn.SetDeadline(time.Now())
		_ = dtlsConn.SetDeadline(time.Now())
	})

	var addr atomic.Value
	go func() {
		defer wg.Done()
		defer dtlscancel()
		buf := make([]byte, 4096)
		for {
			select {
			case <-dtlsctx.Done():
				return
			default:
			}
			n, addr1, err1 := listenConn.ReadFrom(buf)
			if err1 != nil {
				return
			}
			addr.Store(addr1)
			r.Metrics.RecordUp(n)
			if _, err1 = dtlsConn.Write(buf[:n]); err1 != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer dtlscancel()
		buf := make([]byte, 4096)
		for {
			select {
			case <-dtlsctx.Done():
				return
			default:
			}
			n, err1 := dtlsConn.Read(buf)
			if err1 != nil {
				return
			}
			r.Metrics.RecordDown(n)
			addr1, ok := addr.Load().(net.Addr)
			if !ok {
				return
			}
			if _, err1 = listenConn.WriteTo(buf[:n], addr1); err1 != nil {
				return
			}
		}
	}()

	wg.Wait()
	_ = listenConn.SetDeadline(time.Time{})
	_ = dtlsConn.SetDeadline(time.Time{})
}

func (r *Relay) oneDtlsConnectionLoop(ctx context.Context, peer *net.UDPAddr, listenConnChan <-chan net.PacketConn, connchan chan<- net.PacketConn, okchan chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case listenConn := <-listenConnChan:
			c := make(chan error)
			go r.oneDtlsConnection(ctx, peer, listenConn, connchan, okchan, c)
			if err := <-c; err != nil {
				r.logf(fmt.Sprintf("DTLS: %s", err))
			}
		}
	}
}

// --- TURN functions ---

type turnParams struct {
	host     string
	port     string
	link     string
	udp      bool
	getCreds getCredsFunc
}

type connectedUDPConn struct {
	*net.UDPConn
}

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}

func (r *Relay) oneTurnConnection(ctx context.Context, tp *turnParams, peer *net.UDPAddr, conn2 net.PacketConn, c chan<- error) {
	var err error
	defer func() { c <- err }()

	r.logf("Fetching TURN credentials...")
	user, pass, url, err1 := tp.getCreds(tp.link)
	if err1 != nil {
		err = fmt.Errorf("get TURN creds: %s", err1)
		return
	}

	urlhost, urlport, err1 := net.SplitHostPort(url)
	if err1 != nil {
		err = fmt.Errorf("parse TURN addr: %s", err1)
		return
	}
	if tp.host != "" {
		urlhost = tp.host
	}
	if tp.port != "" {
		urlport = tp.port
	}

	turnServerAddr := net.JoinHostPort(urlhost, urlport)
	turnServerUdpAddr, err1 := net.ResolveUDPAddr("udp", turnServerAddr)
	if err1 != nil {
		err = fmt.Errorf("resolve TURN: %s", err1)
		return
	}
	turnServerAddr = turnServerUdpAddr.String()
	r.logf(fmt.Sprintf("TURN server: %s", turnServerAddr))

	var cfg *turn.ClientConfig
	var turnConn net.PacketConn
	var d net.Dialer
	ctx1, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if tp.udp {
		conn, err2 := net.DialUDP("udp", nil, turnServerUdpAddr)
		if err2 != nil {
			err = fmt.Errorf("dial TURN: %s", err2)
			return
		}
		defer func() { _ = conn.Close() }()
		turnConn = &connectedUDPConn{conn}
	} else {
		conn, err2 := d.DialContext(ctx1, "tcp", turnServerAddr)
		if err2 != nil {
			err = fmt.Errorf("dial TURN: %s", err2)
			return
		}
		defer func() { _ = conn.Close() }()
		turnConn = turn.NewSTUNConn(conn)
	}

	var addrFamily turn.RequestedAddressFamily
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	} else {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	cfg = &turn.ClientConfig{
		STUNServerAddr:         turnServerAddr,
		TURNServerAddr:         turnServerAddr,
		Conn:                   turnConn,
		Username:               user,
		Password:               pass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          logging.NewDefaultLoggerFactory(),
	}

	client, err1 := turn.NewClient(cfg)
	if err1 != nil {
		err = fmt.Errorf("TURN client: %s", err1)
		return
	}
	defer client.Close()

	err1 = client.Listen()
	if err1 != nil {
		err = fmt.Errorf("TURN listen: %s", err1)
		return
	}

	relayConn, err1 := client.Allocate()
	if err1 != nil {
		err = fmt.Errorf("TURN allocate: %s", err1)
		return
	}
	defer func() { _ = relayConn.Close() }()

	r.logf(fmt.Sprintf("Relay address: %s", relayConn.LocalAddr().String()))

	wg := sync.WaitGroup{}
	wg.Add(2)
	// #1 fix: use parent ctx instead of context.Background()
	turnctx, turncancel := context.WithCancel(ctx)
	context.AfterFunc(turnctx, func() {
		_ = relayConn.SetDeadline(time.Now())
		_ = conn2.SetDeadline(time.Now())
	})

	var addr atomic.Value
	go func() {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 4096)
		for {
			select {
			case <-turnctx.Done():
				return
			default:
			}
			n, addr1, err1 := conn2.ReadFrom(buf)
			if err1 != nil {
				return
			}
			addr.Store(addr1)
			if _, err1 = relayConn.WriteTo(buf[:n], peer); err1 != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer turncancel()
		buf := make([]byte, 4096)
		for {
			select {
			case <-turnctx.Done():
				return
			default:
			}
			n, _, err1 := relayConn.ReadFrom(buf)
			if err1 != nil {
				return
			}
			addr1, ok := addr.Load().(net.Addr)
			if !ok {
				return
			}
			if _, err1 = conn2.WriteTo(buf[:n], addr1); err1 != nil {
				return
			}
		}
	}()

	wg.Wait()
	_ = relayConn.SetDeadline(time.Time{})
	_ = conn2.SetDeadline(time.Time{})
}

func (r *Relay) oneTurnConnectionLoop(ctx context.Context, tp *turnParams, peer *net.UDPAddr, connchan <-chan net.PacketConn, t <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case conn2 := <-connchan:
			select {
			case <-t:
				c := make(chan error)
				go r.oneTurnConnection(ctx, tp, peer, conn2, c)
				if err := <-c; err != nil {
					r.logf(fmt.Sprintf("TURN: %s", err))
				}
			default:
			}
		}
	}
}
