package label

import (
	"fmt"
	"sync"
)

// EngineCache is the engine's lifecycle: the one place a caller asks "what is
// the engine for the dictionary in force right now?".
//
// The acronym list is an operator setting (#684), so the dictionary changes
// while the process runs. That makes an engine a value with a life rather than
// a constant, and it raises the question of what happens to a rule already
// compiled against the old one. The answer this type encodes is that NOTHING is
// carried across: a change produces a NEW engine, the old one is untouched, and
// a rule holding the old engine's FuncMap keeps titling the old way for as long
// as it lives. Callers on the write path re-parse every render, so the longest a
// stale dictionary survives is one render.
//
// The cache key IS the dictionary. That is the whole design and it is worth
// stating plainly, because the obvious alternative (a generation counter bumped
// by whoever writes the setting) has one failure mode this cannot have: a writer
// that forgets to bump, which yields a cache that is correct until the day
// somebody adds a second write path. A dictionary that has not changed cannot
// produce a different key, and one that has cannot produce the same key.
//
// What it deliberately does NOT do is decide when to re-read the SETTING. That
// belongs to the caller, which knows whether it is rendering one row or fifteen
// thousand; the bulk recompute (#685) resolves the list once and reuses the
// engine for the whole run.
//
// The zero value is ready to use, and holds the engine with no dictionary until
// the first call says otherwise.
type EngineCache struct {
	mu  sync.Mutex
	key string
	eng *Engine
}

// For returns the engine for acronyms, building one only when the dictionary
// differs from the engine already held.
//
// The lock is held across the build rather than double-checked, because the
// build is a small map and the contended case is two callers wanting the SAME
// engine, which is precisely the case a second build would waste.
func (c *EngineCache) For(acronyms []string) *Engine {
	key := dictKey(acronyms)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eng != nil && c.key == key {
		return c.eng
	}
	c.key, c.eng = key, New(acronyms)
	return c.eng
}

// dictKey renders a dictionary as a comparable key. %q quotes every element, so
// a separator cannot be forged from inside one and two different lists cannot
// collide on one key.
func dictKey(acronyms []string) string { return fmt.Sprintf("%q", acronyms) }
