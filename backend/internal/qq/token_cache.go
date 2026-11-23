package qq

import (
	"crypto/sha256"
	"sync"
	"time"
)

type tokenEntry struct {
	mu    sync.Mutex
	until time.Time
	value string
}

var tokenEntries sync.Map

func credentialKey(c *Client) [32]byte {
	return sha256.Sum256([]byte(c.TokenURL + "\x00" + c.AppID + "\x00" + c.AppSecret))
}
func cachedToken(key [32]byte) *tokenEntry {
	v, _ := tokenEntries.LoadOrStore(key, &tokenEntry{})
	return v.(*tokenEntry)
}
