package creds

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// CachedCreds wraps a GetCredsFunc and caches results for the given TTL.
// TURN credentials are valid for the entire VK call session (~10-30 min).
// This avoids hammering VK API on every TURN reconnect.
//
// Captcha errors trigger a separate, longer "captchaCooldown" — when VK starts
// returning manual-verification challenges, immediately retrying makes things
// worse (VK escalates the rate limit). We back off for 5 minutes by default.
type CachedCreds struct {
	mu              sync.Mutex
	user            string
	pass            string
	addr            string
	fetchedAt       time.Time
	ttl             time.Duration
	cooldown        time.Duration
	captchaCooldown time.Duration
	lastFetch       time.Time
	captchaUntil    time.Time // soft block — Get() returns CaptchaManualNeeded until this time
	lastCaptchaErr  *CaptchaError
	fetch           GetCredsFunc
	logf            LogFunc
}

// NewCachedCreds wraps a creds function with caching.
// ttl: how long to reuse cached creds (e.g. 5 minutes).
// cooldown: minimum time between API calls (e.g. 30 seconds).
// captchaCooldown defaults to 5 minutes — when VK demands manual verification,
// we don't hit the API again until this expires (re-pinging just escalates the ban).
func NewCachedCreds(fetch GetCredsFunc, ttl, cooldown time.Duration, logf LogFunc) GetCredsFunc {
	return NewCachedCredsStruct(fetch, ttl, cooldown, logf).Get
}

// NewCachedCredsStruct returns the underlying *CachedCreds so callers can call
// Invalidate() / ClearCaptchaCooldown() on it.
func NewCachedCredsStruct(fetch GetCredsFunc, ttl, cooldown time.Duration, logf LogFunc) *CachedCreds {
	return &CachedCreds{
		ttl:             ttl,
		cooldown:        cooldown,
		captchaCooldown: 5 * time.Minute,
		fetch:           fetch,
		logf:            logf,
	}
}

func (c *CachedCreds) Get(link string) (string, string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached if still valid
	if c.user != "" && time.Since(c.fetchedAt) < c.ttl {
		return c.user, c.pass, c.addr, nil
	}

	// Captcha cooldown: if VK recently demanded manual verification, don't hit API.
	// Returning the cached CaptchaError lets the GUI keep showing the same redirect
	// URL instead of generating a new one (and escalating the rate limit).
	if !c.captchaUntil.IsZero() && time.Now().Before(c.captchaUntil) {
		wait := time.Until(c.captchaUntil).Round(time.Second)
		c.logf(fmt.Sprintf("Captcha cooldown active — %s remaining. Pass the captcha in your browser, or wait.", wait))
		if c.lastCaptchaErr != nil {
			return "", "", "", c.lastCaptchaErr
		}
		return "", "", "", &CaptchaError{
			Kind:    CaptchaManualNeeded,
			Message: fmt.Sprintf("captcha cooldown: wait %s", wait),
		}
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
		// On manual-captcha errors, install a longer cooldown so the next Connect
		// doesn't immediately re-hit VK and escalate the rate limit.
		var ce *CaptchaError
		if errors.As(err, &ce) && ce.Kind == CaptchaManualNeeded {
			c.captchaUntil = time.Now().Add(c.captchaCooldown)
			c.lastCaptchaErr = ce
			c.logf(fmt.Sprintf("Captcha cooldown armed for %s. Complete the verification in your browser before retrying.", c.captchaCooldown))
		}
		return "", "", "", err
	}

	// Successful fetch — clear any captcha cooldown
	c.captchaUntil = time.Time{}
	c.lastCaptchaErr = nil

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

// ClearCaptchaCooldown lets the next Get() try VK API again before the cooldown expires.
// Call this when the user has confirmed they completed the captcha in a browser.
func (c *CachedCreds) ClearCaptchaCooldown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.captchaUntil = time.Time{}
	c.lastCaptchaErr = nil
}
