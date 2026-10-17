package mergeproof

import "sync"

// memo computes each key's value once and shares it with concurrent callers
// of the same key (single flight), without holding its lock while the value
// is computed: git runs for one key never block lookups of another.
type memo[V any] struct {
	mu    sync.Mutex
	calls map[string]*memoCall[V]
}

type memoCall[V any] struct {
	done chan struct{}
	val  V
	ok   bool // false when the computation panicked
}

// do returns the value for key, computing it with fn on first use. If fn
// panics, the panic propagates to its caller, the key is forgotten, and the
// callers that waited for it get aborted (fail closed: never a zero value
// that could read as an answer).
func (m *memo[V]) do(key string, fn func() V, aborted V) V {
	m.mu.Lock()
	if m.calls == nil {
		m.calls = make(map[string]*memoCall[V])
	}
	if c, found := m.calls[key]; found {
		m.mu.Unlock()
		<-c.done
		if !c.ok {
			return aborted
		}
		return c.val
	}
	c := &memoCall[V]{done: make(chan struct{})}
	m.calls[key] = c
	m.mu.Unlock()

	defer func() {
		if !c.ok {
			m.mu.Lock()
			delete(m.calls, key)
			m.mu.Unlock()
		}
		close(c.done)
	}()
	c.val = fn()
	c.ok = true
	return c.val
}
