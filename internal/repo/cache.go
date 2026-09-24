package repo

import (
	"container/list"
	"sync"
)

// How many coloured files are kept. A review walks a few dozen files and comes
// back to them; past that the oldest goes, and colouring it again costs about
// a millisecond per kilobyte.
const CacheEntries = 64

// key is what a coloured copy is kept under. The id of a blob changes with its
// content and with nothing else, so an entry never goes stale: a file edited
// in place gets a new id and therefore a new entry, and nothing has to be
// invalidated by hand.
type key struct {
	oid   string
	lexer string
}

type entry struct {
	key   key
	lines [][]Span
	name  string
}

// Cache keeps coloured files by the id of the blob they were coloured from.
type Cache struct {
	mu    sync.Mutex
	order *list.List
	items map[key]*list.Element
	limit int

	hits, misses int
}

// NewCache returns a cache of at most n entries.
func NewCache(n int) *Cache {
	if n <= 0 {
		n = CacheEntries
	}
	return &Cache{order: list.New(), items: make(map[key]*list.Element, n), limit: n}
}

// Painted returns the coloured lines of a blob, colouring it on the first ask.
//
// The lexer name is part of the key rather than derived inside: the same blob
// read as two different languages is two answers, and a file renamed from .txt
// to .go is the same content with a different reading.
func (c *Cache) Painted(oid, path, text string) ([][]Span, string) {
	lines, name, _ := c.PaintedBy(oid, path, func() (string, bool) { return text, true })
	return lines, name
}

// PaintedBy is Painted for a blob whose text costs a trip to fetch: the text
// is asked for only when the blob is not kept yet. A text that could not be
// had is an answer of false, and nothing is kept under the id — the next ask
// tries again rather than finding an empty colouring that was never true.
func (c *Cache) PaintedBy(oid, path string, text func() (string, bool)) ([][]Span, string, bool) {
	lexer := Lexer(path)
	if oid == "" || lexer == "" {
		body, ok := text()
		if !ok {
			return nil, "", false
		}
		lines, name := Paint(path, body)
		return lines, name, true
	}
	k := key{oid: oid, lexer: lexer}

	c.mu.Lock()
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		found := el.Value.(*entry)
		c.hits++
		c.mu.Unlock()
		return found.lines, found.name, true
	}
	c.misses++
	c.mu.Unlock()

	body, ok := text()
	if !ok {
		return nil, "", false
	}
	// Painted outside the lock: colouring a large file takes long enough that
	// holding the lock would queue every other reader behind it, and painting
	// the same file twice costs less than that queue.
	lines, name := Paint(path, body)

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*entry).lines, el.Value.(*entry).name, true
	}
	el := c.order.PushFront(&entry{key: k, lines: lines, name: name})
	c.items[k] = el
	for c.order.Len() > c.limit {
		last := c.order.Back()
		if last == nil {
			break
		}
		c.order.Remove(last)
		delete(c.items, last.Value.(*entry).key)
	}
	return lines, name, true
}

// Stat returns how the cache has been doing, for the health screen and for a
// test that wants to know a second read did not repaint.
func (c *Cache) Stat() (entries, hits, misses int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len(), c.hits, c.misses
}
