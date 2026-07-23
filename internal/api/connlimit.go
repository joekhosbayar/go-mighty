package api

import (
	"errors"
	"sync"
)

var (
	errTooManyUserConns = errors.New("too many concurrent connections for this user")
	errTooManyIPConns   = errors.New("too many concurrent connections from this address")
)

// connRegistry counts live WebSocket connections per user and per source IP
// (spec Section 3, Layer 2: ~3/user, ~20/IP). A cap of zero disables that
// dimension.
type connRegistry struct {
	mu         sync.Mutex
	perUser    map[string]int
	perIP      map[string]int
	maxPerUser int
	maxPerIP   int
}

func newConnRegistry(maxPerUser, maxPerIP int) *connRegistry {
	return &connRegistry{
		perUser:    make(map[string]int),
		perIP:      make(map[string]int),
		maxPerUser: maxPerUser,
		maxPerIP:   maxPerIP,
	}
}

// acquire reserves a slot for one connection. The returned release must be
// called exactly once when the connection ends; it is idempotent-safe only in
// the sense that callers should defer it immediately after a nil-error
// acquire.
func (c *connRegistry) acquire(userID, ip string) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxPerUser > 0 && c.perUser[userID] >= c.maxPerUser {
		return nil, errTooManyUserConns
	}

	if c.maxPerIP > 0 && c.perIP[ip] >= c.maxPerIP {
		return nil, errTooManyIPConns
	}

	c.perUser[userID]++
	c.perIP[ip]++

	var once sync.Once

	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			// Delete at zero so the maps don't grow without bound across the
			// lifetime of the process.
			if c.perUser[userID]--; c.perUser[userID] <= 0 {
				delete(c.perUser, userID)
			}

			if c.perIP[ip]--; c.perIP[ip] <= 0 {
				delete(c.perIP, ip)
			}
		})
	}, nil
}
