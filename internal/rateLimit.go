package form_mailer

import (
	"sync"
	"time"
)

type Bucket struct {
	tokens int
	ts     time.Time
}

var (
	// simple per-site+ip rate limit
	buckets   = map[string]*Bucket{}
	bucketsMu sync.Mutex
)

func Allow(site, ip string, burst, refillMins int) bool {
	key := site + "|" + ip
	now := time.Now()
	bucketsMu.Lock()
	defer bucketsMu.Unlock()
	b, ok := buckets[key]
	if !ok {
		buckets[key] = &Bucket{tokens: burst, ts: now}
		return true
	}
	// refill per minute
	refills := int(now.Sub(b.ts).Minutes())
	if refills > 0 {
		b.tokens += refills
		if b.tokens > burst {
			b.tokens = burst
		}
		b.ts = now
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}
