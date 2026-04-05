package main

import "github.com/politologhse/good-turn/internal/profile"

func generateConfigString(addr, pass, sni string) string {
	return profile.Generate(addr, pass, sni)
}

func parseConfigString(raw string) (profile.Profile, error) {
	return profile.Parse(raw)
}
