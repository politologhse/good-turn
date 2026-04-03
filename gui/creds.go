package main

import (
	"github.com/politologhse/good-turn/internal/creds"
)

// Re-export types from internal/creds for use in gui package.
type logFunc = creds.LogFunc
type getCredsFunc = creds.GetCredsFunc

var (
	withRetry  = creds.WithRetry
	getVkCreds = creds.GetVkCreds
)
