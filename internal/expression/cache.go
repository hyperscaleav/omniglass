package expression

import (
	"container/list"
	"reflect"
	"sync"
)

// MaxCachedPrograms bounds the program cache. Keys are content-addressed
// (source plus env shape), so an evicted program costs one recompile.
const MaxCachedPrograms = 1024

type cacheKey struct {
	source string
	env    reflect.Type
	result Result
}

type cacheEntry struct {
	key  cacheKey
	prog *Program
}

// cache is a least-recently-used map of compiled programs, safe for
// concurrent use.
type cache struct {
	mu    sync.Mutex
	size  int
	order *list.List // front is most recent
	items map[cacheKey]*list.Element
}

func newCache(size int) *cache {
	return &cache{size: size, order: list.New(), items: map[cacheKey]*list.Element{}}
}

func (c *cache) get(k cacheKey) (*Program, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).prog, true
}

// put stores p under k and returns the program now cached there: p, or the
// one a concurrent compile of the same key stored first, so every caller of
// one key shares one program.
func (c *cache) put(k cacheKey, p *Program) *Program {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*cacheEntry).prog
	}
	c.items[k] = c.order.PushFront(&cacheEntry{key: k, prog: p})
	for c.order.Len() > c.size {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).key)
	}
	return p
}

func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
