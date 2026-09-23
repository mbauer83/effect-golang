package cache

// What one process keeps to hand.

import (
	"container/list"
	"sync"
	"time"
)

// LRU is a bounded, dated set of values one process keeps.
//
// Bounded and dated, both, and neither alone is enough: bounded so a long
// afternoon cannot grow it without limit, and dated so nothing is served for
// longer than it is worth serving. A map with expiry grows; a bounded map
// serves last week's answer.
//
// Typed, so nothing is encoded to be put in and decoded to be taken out --
// which is the whole reason to have one of these in front of a shared store.
// Values are shared with whoever put them in and whoever takes them out, so
// what goes in should be a value rather than something with a pointer into it.
type LRU[A any] struct {
	mutex    sync.Mutex
	most     int
	now      func() time.Time
	elements map[string]*list.Element
	order    *list.List
	about    map[string]map[string]struct{}
}

// lruEntry is one entry: what it holds, what it is about, and when it stops
// being worth having.
type lruEntry[A any] struct {
	key   string
	about string
	value A
	until time.Time
}

// NewLRU is a cache of at most this many entries.
//
// The clock is a parameter because a thing with a lifetime is a thing a test
// has to be able to move, and waiting out a twelve-hour lifetime is not a
// test. Pass time.Now unless you are one.
func NewLRU[A any](most int, now func() time.Time) *LRU[A] {
	if most < 1 {
		most = 1
	}
	if now == nil {
		now = time.Now
	}
	return &LRU[A]{
		most:     most,
		now:      now,
		elements: make(map[string]*list.Element, most),
		order:    list.New(),
		about:    map[string]map[string]struct{}{},
	}
}

// Get is what is held under a key, and whether anything still worth serving
// is.
//
// A read counts as use, which is what makes this least-recently-used rather
// than least-recently-written: the forty films being looked at all afternoon
// stay, and the one somebody opened once does not.
func (cache *LRU[A]) Get(key string) (A, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	element, found := cache.elements[key]
	if !found {
		var zero A
		return zero, false
	}
	entry := element.Value.(*lruEntry[A])
	if cache.now().After(entry.until) {
		cache.drop(element)
		var zero A
		return zero, false
	}
	cache.order.MoveToFront(element)
	return entry.value, true
}

// Put stores a value under a key, notes what it is about, and evicts the
// least recently used if that puts it over its bound.
func (cache *LRU[A]) Put(key string, about string, a A, fresh time.Duration) {
	if key == "" || fresh <= 0 {
		return
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if element, found := cache.elements[key]; found {
		cache.drop(element)
	}
	entry := &lruEntry[A]{key: key, about: about, value: a, until: cache.now().Add(fresh)}
	cache.elements[key] = cache.order.PushFront(entry)
	if about != "" {
		keys, listed := cache.about[about]
		if !listed {
			keys = map[string]struct{}{}
			cache.about[about] = keys
		}
		keys[key] = struct{}{}
	}
	for cache.order.Len() > cache.most {
		cache.drop(cache.order.Back())
	}
}

// Invalidate drops every entry about one subject.
func (cache *LRU[A]) Invalidate(subject string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	for key := range cache.about[subject] {
		if element, found := cache.elements[key]; found {
			cache.drop(element)
		}
	}
	delete(cache.about, subject)
}

// Len is how many entries are held, which is what a metric reports. Entries
// past their date are counted until something asks for them, because this
// sweeps nothing: an entry nobody asks for costs a map slot and its eviction
// is the bound's business.
func (cache *LRU[A]) Len() int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return cache.order.Len()
}

// drop removes one entry and forgets that its subject had it. Called with the
// lock held.
func (cache *LRU[A]) drop(element *list.Element) {
	entry := element.Value.(*lruEntry[A])
	cache.order.Remove(element)
	delete(cache.elements, entry.key)
	if keys, listed := cache.about[entry.about]; listed {
		delete(keys, entry.key)
		if len(keys) == 0 {
			delete(cache.about, entry.about)
		}
	}
}
