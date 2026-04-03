package main

import "fmt"

type linuxProxy struct{}

func NewSystemProxy() SystemProxy {
	return &linuxProxy{}
}

func (p *linuxProxy) Enable(socksPort int) error {
	return fmt.Errorf("system proxy not supported on Linux, configure SOCKS5 manually on 127.0.0.1:%d", socksPort)
}

func (p *linuxProxy) Disable() error {
	return nil
}
