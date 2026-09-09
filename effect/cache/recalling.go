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
	mutex sync.Mutex
	most  int
	now   func() time.Time
	held  map[string]*list.Element
	order *list.List
	about map[string]map[string]struct{}
}

// recalled is one entry: what it holds, what it is about, and when it stops
// being worth having.
type recalled[A any] struct {
	key   string
	about string
	held  A
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
		most:  most,
		now:   now,
		held:  make(map[string]*list.Element, most),
		order: list.New(),
		about: map[string]map[string]struct{}{},
	}
}

// Get is what is held under a key, and whether anything still worth serving
// is.
//
// A read counts as use, which is what makes this least-recently-used rather
// than least-recently-written: the forty films being looked at all afternoon
// stay, and the one somebody opened once does not.
func (memory *LRU[A]) Get(key string) (A, bool) {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	element, found := memory.held[key]
	if !found {
		var nothing A
		return nothing, false
	}
	entry := element.Value.(*recalled[A])
	if memory.now().After(entry.until) {
		memory.drop(element)
		var nothing A
		return nothing, false
	}
	memory.order.MoveToFront(element)
	return entry.held, true
}

// Put stores a value under a key, notes what it is about, and evicts the
// least recently used if that puts it over its bound.
func (memory *LRU[A]) Put(key string, about string, held A, fresh time.Duration) {
	if key == "" || fresh <= 0 {
		return
	}
	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	if element, found := memory.held[key]; found {
		memory.drop(element)
	}
	entry := &recalled[A]{key: key, about: about, held: held, until: memory.now().Add(fresh)}
	memory.held[key] = memory.order.PushFront(entry)
	if about != "" {
		keys, listed := memory.about[about]
		if !listed {
			keys = map[string]struct{}{}
			memory.about[about] = keys
		}
		keys[key] = struct{}{}
	}
	for memory.order.Len() > memory.most {
		memory.drop(memory.order.Back())
	}
}

// Invalidate drops every entry about one subject.
func (memory *LRU[A]) Invalidate(subject string) {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	for key := range memory.about[subject] {
		if element, found := memory.held[key]; found {
			memory.drop(element)
		}
	}
	delete(memory.about, subject)
}

// Len is how many entries are held, which is what a metric reports. Entries
// past their date are counted until something asks for them, because this
// sweeps nothing: an entry nobody asks for costs a map slot and its eviction
// is the bound's business.
func (memory *LRU[A]) Len() int {
	memory.mutex.Lock()
	defer memory.mutex.Unlock()
	return memory.order.Len()
}

// drop removes one entry and forgets that its subject had it. Called with the
// lock held.
func (memory *LRU[A]) drop(element *list.Element) {
	entry := element.Value.(*recalled[A])
	memory.order.Remove(element)
	delete(memory.held, entry.key)
	if keys, listed := memory.about[entry.about]; listed {
		delete(keys, entry.key)
		if len(keys) == 0 {
			delete(memory.about, entry.about)
		}
	}
}
