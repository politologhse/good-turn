package creds

import (
	"fmt"
	"sync"
	"time"
)

// CachedCreds wraps a GetCredsFunc and caches results for the given TTL.
// TURN credentials are valid for the entire VK call session (~10-30 min).
// This avoids hammering VK API on every TURN reconnect.
type CachedCreds struct {
	mu        sync.Mutex
	user      string
	pass      string
	addr      string
	fetchedAt time.Time
	ttl       time.Duration
	cooldown  time.Duration
	lastFetch time.Time
	fetch     GetCredsFunc
	logf      LogFunc
}

// NewCachedCreds wraps a creds function with caching.
// ttl: how long to reuse cached creds (e.g. 5 minutes).
// cooldown: minimum time between API calls (e.g. 30 seconds).
func NewCachedCreds(fetch GetCredsFunc, ttl, cooldown time.Duration, logf LogFunc) GetCredsFunc {
	c := &CachedCreds{
		ttl:      ttl,
		cooldown: cooldown,
		fetch:    fetch,
		logf:     logf,
	}
	return c.Get
}

func (c *CachedCreds) Get(link string) (string, string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached if still valid
	if c.user != "" && time.Since(c.fetchedAt) < c.ttl {
		return c.user, c.pass, c.addr, nil
	}

	// Rate limit: don't hit API too fast
	if !c.lastFetch.IsZero() && time.Since(c.lastFetch) < c.cooldown {
		wait := c.cooldown - time.Since(c.lastFetch)
		c.logf(fmt.Sprintf("Rate limit: waiting %s before next VK API call", wait.Round(time.Second)))
		c.mu.Unlock()
		time.Sleep(wait)
		c.mu.Lock()
	}

	c.lastFetch = time.Now()

	// Fetch fresh creds (this might take 5-10s with captcha)
	c.mu.Unlock()
	user, pass, addr, err := c.fetch(link)
	c.mu.Lock()

	if err != nil {
		return "", "", "", err
	}

	c.user = user
	c.pass = pass
	c.addr = addr
	c.fetchedAt = time.Now()
	c.logf(fmt.Sprintf("Credentials cached for %s", c.ttl))
	return user, pass, addr, nil
}

// Invalidate forces the next Get() to fetch fresh creds.
// Call this when TURN allocation fails with auth error.
func (c *CachedCreds) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.user = ""
	c.pass = ""
	c.addr = ""
	c.fetchedAt = time.Time{}
}
