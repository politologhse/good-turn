package main

type SystemProxy interface {
	Enable(socksPort int) error
	Disable() error
}
