package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:56000", "listen on ip:port")
	connect := flag.String("connect", "", "connect to ip:port (e.g. 127.0.0.1:443)")
	certFile := flag.String("cert", "", "path to DTLS certificate PEM (auto-generated if empty)")
	keyFile := flag.String("key", "", "path to DTLS private key PEM (auto-generated if empty)")

	// Utility flags
	genConfig := flag.Bool("generate-config", false, "generate gt:// config string and exit")
	healthAddr := flag.String("health-addr", "", "TCP address for healthcheck endpoint (e.g. 127.0.0.1:56001)")
	healthProbe := flag.String("health-probe", "", "probe a TCP healthcheck endpoint and exit (e.g. 127.0.0.1:56001)")
	gcAddr := flag.String("addr", "", "server address for config (e.g. 185.1.2.3:56000)")
	gcPass := flag.String("pass", "", "hysteria2 password for config")
	gcSNI := flag.String("sni", "hy2", "SNI for config")

	flag.Parse()

	// Config generation mode
	if *genConfig {
		// #7 fix: prefer GT_PASS env var over -pass flag (avoids ps aux leak)
		pass := *gcPass
		if envPass := os.Getenv("GT_PASS"); envPass != "" {
			pass = envPass
		}
		if *gcAddr == "" || pass == "" {
			fmt.Fprintf(os.Stderr, "Usage: server -generate-config -addr <ip:port> -pass <password> [-sni <domain>]\n")
			fmt.Fprintf(os.Stderr, "  Password can also be set via GT_PASS env var\n")
			os.Exit(1)
		}
		fmt.Println(generateConfigString(*gcAddr, pass, *gcSNI))
		return
	}

	// Health probe mode: probe a running server's healthcheck endpoint
	if *healthProbe != "" {
		conn, err := net.DialTimeout("tcp", *healthProbe, 3*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s unreachable: %s\n", *healthProbe, err)
			os.Exit(1)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 16)
		n, _ := conn.Read(buf)
		_ = conn.Close()
		if n > 0 && string(buf[:n]) == "OK\n" {
			fmt.Printf("OK: %s healthy\n", *healthProbe)
			return
		}
		fmt.Fprintf(os.Stderr, "FAIL: %s did not respond OK\n", *healthProbe)
		os.Exit(1)
	}

	if *connect == "" {
		log.Panicf("-connect is required (e.g. -connect 127.0.0.1:443)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signalChan
		log.Printf("Terminating...\n")
		cancel()
		<-signalChan
		log.Fatalf("Exit...\n")
	}()

	addr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		panic(err)
	}

	// Load or generate DTLS certificate
	var certificate tls.Certificate
	if *certFile != "" && *keyFile != "" {
		cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to load certificate: %s", err)
		}
		certificate = cert
		log.Printf("Loaded DTLS certificate from %s", *certFile)
	} else {
		cert, genErr := selfsign.GenerateSelfSigned()
		if genErr != nil {
			panic(genErr)
		}
		certificate = cert
		log.Printf("Generated ephemeral DTLS certificate")
	}

	// Optional TCP healthcheck endpoint (returns "OK\n" to any connection)
	if *healthAddr != "" {
		hln, err := net.Listen("tcp", *healthAddr)
		if err != nil {
			log.Fatalf("healthcheck listen %s: %s", *healthAddr, err)
		}
		log.Printf("Healthcheck listening on %s", *healthAddr)
		go func() {
			for {
				c, err := hln.Accept()
				if err != nil {
					return
				}
				go func(conn net.Conn) {
					defer func() { _ = conn.Close() }()
					_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
					_, _ = conn.Write([]byte("OK\n"))
				}(c)
			}
		}()
		context.AfterFunc(ctx, func() { _ = hln.Close() })
	}

	//
	// Everything below is the pion-DTLS API! Thanks for using it ❤️.
	//

	// Prepare the configuration of the DTLS connection
	config := &dtls.Config{
		Certificates:          []tls.Certificate{certificate},
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.RandomCIDGenerator(8),
	}

	// Connect to a DTLS server
	listener, err := dtls.Listen("udp", addr, config)
	if err != nil {
		panic(err)
	}
	context.AfterFunc(ctx, func() {
		if err = listener.Close(); err != nil {
			panic(err)
		}
	})

	fmt.Println("Listening")

	wg1 := sync.WaitGroup{}
	for {
		select {
		case <-ctx.Done():
			wg1.Wait()
			return
		default:
		}
		// Wait for a connection.
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		wg1.Add(1)
		go func(conn net.Conn) {
			defer wg1.Done()
			defer func() {
				if closeErr := conn.Close(); closeErr != nil {
					log.Printf("failed to close incoming connection: %s", closeErr)
				}
			}()
			var err error = nil
			log.Printf("Connection from %s\n", conn.RemoteAddr())
			// `conn` is of type `net.Conn` but may be casted to `dtls.Conn`
			// using `dtlsConn := conn.(*dtls.Conn)` in order to to expose
			// functions like `ConnectionState` etc.

			// Perform the handshake with a 30-second timeout
			ctx1, cancel1 := context.WithTimeout(ctx, 30*time.Second)
			dtlsConn, ok := conn.(*dtls.Conn)
			if !ok {
				log.Println("Type error")
				cancel1()
				return
			}
			log.Println("Start handshake")
			if err = dtlsConn.HandshakeContext(ctx1); err != nil {
				log.Println(err)
				cancel1()
				return
			}
			cancel1()
			log.Println("Handshake done")

			serverConn, err := net.Dial("udp", *connect)
			if err != nil {
				log.Println(err)
				return
			}
			defer func() {
				if err = serverConn.Close(); err != nil {
					log.Printf("failed to close outgoing connection: %s", err)
					return
				}
			}()

			var wg sync.WaitGroup
			wg.Add(2)
			ctx2, cancel2 := context.WithCancel(ctx)
			context.AfterFunc(ctx2, func() {
				if err := conn.SetDeadline(time.Now()); err != nil {
					log.Printf("failed to set incoming deadline: %s", err)
				}
				if err := serverConn.SetDeadline(time.Now()); err != nil {
					log.Printf("failed to set outgoing deadline: %s", err)
				}
			})
			go func() {
				defer wg.Done()
				defer cancel2()
				buf := make([]byte, 4096)
				for {
					select {
					case <-ctx2.Done():
						return
					default:
					}
					if err1 := conn.SetReadDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
					n, err1 := conn.Read(buf)
					if err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}

					if err1 := serverConn.SetWriteDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
					_, err1 = serverConn.Write(buf[:n])
					if err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				defer cancel2()
				buf := make([]byte, 4096)
				for {
					select {
					case <-ctx2.Done():
						return
					default:
					}
					if err1 := serverConn.SetReadDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
					n, err1 := serverConn.Read(buf)
					if err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}

					if err1 := conn.SetWriteDeadline(time.Now().Add(time.Minute * 30)); err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
					_, err1 = conn.Write(buf[:n])
					if err1 != nil {
						log.Printf("Failed: %s", err1)
						return
					}
				}
			}()
			wg.Wait()
			log.Printf("Connection closed: %s\n", conn.RemoteAddr())
		}(conn)
	}
}
