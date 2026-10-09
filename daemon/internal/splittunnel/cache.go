package splittunnel

import (
	"container/list"
	"time"
)

type cacheEntry struct {
	key     flowKey
	isn     uint32
	expires time.Time
}

// fifoCache bounds cache-type entries (tunnel verdicts, ended bypass keys): entries are
// evicted oldest-first and eviction only costs a re-lookup.
type fifoCache struct {
	max int
	m   map[flowKey]*list.Element
	l   list.List
}

func (c *fifoCache) init(max int) {
	c.max = max
	c.m = make(map[flowKey]*list.Element)
}

func (c *fifoCache) get(k flowKey, now time.Time) *cacheEntry {
	el := c.m[k]
	if el == nil {
		return nil
	}
	ce := el.Value.(*cacheEntry)
	if now.After(ce.expires) {
		c.l.Remove(el)
		delete(c.m, k)
		return nil
	}
	return ce
}

func (c *fifoCache) put(k flowKey, isn uint32, expires time.Time) {
	if el := c.m[k]; el != nil {
		c.l.Remove(el)
	}
	for c.max > 0 && c.l.Len() >= c.max {
		front := c.l.Front()
		delete(c.m, front.Value.(*cacheEntry).key)
		c.l.Remove(front)
	}
	c.m[k] = c.l.PushBack(&cacheEntry{key: k, isn: isn, expires: expires})
}

func (c *fifoCache) remove(k flowKey) {
	if el := c.m[k]; el != nil {
		c.l.Remove(el)
		delete(c.m, k)
	}
}

// sweep drops expired entries from the front up to the first live one; entries are put in
// roughly expiry order and get re-checks expiry, so stragglers only cost memory briefly.
func (c *fifoCache) sweep(now time.Time) {
	for el := c.l.Front(); el != nil; el = c.l.Front() {
		ce := el.Value.(*cacheEntry)
		if !now.After(ce.expires) {
			return
		}
		c.l.Remove(el)
		delete(c.m, ce.key)
	}
}

func (c *fifoCache) len() int { return c.l.Len() }
