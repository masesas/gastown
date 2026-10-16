package daemon

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/tmux"

	beadsdk "github.com/steveyegge/beads"
)

// lazyStoreDaemon builds a Daemon whose convoy manager has no stores at start
// and opens them lazily through opener (simulating Dolt not ready at startup).
func lazyStoreDaemon(t *testing.T, opener func() map[string]beadsdk.Storage) *Daemon {
	t.Helper()
	return &Daemon{
		config:        &Config{TownRoot: t.TempDir()},
		logger:        log.New(io.Discard, "", 0),
		ctx:           context.Background(),
		tmux:          tmux.NewTmux(),
		convoyManager: NewConvoyManager("", func(string, ...interface{}) {}, "gt", 0, nil, opener, nil),
	}
}

func TestLazyOpenedStoresVisibleToIdleAndActiveWork(t *testing.T) {
	ready := false
	d := lazyStoreDaemon(t, func() map[string]beadsdk.Storage {
		if !ready {
			return nil
		}
		return map[string]beadsdk.Storage{
			"hq": &searchStorage{results: map[string][]*beadsdk.Issue{"in_progress": {{ID: "hq-1"}}}},
		}
	})

	// Before the opener succeeds: fail-safe (unavailable / active).
	if snap := d.collectIdleSnapshot(); snap.Err == nil || !strings.Contains(snap.Err.Error(), `beads store "hq" unavailable`) {
		t.Fatalf("want hq unavailable before open, got %v", snap.Err)
	}
	if !d.hasActiveWork() {
		t.Fatal("hasActiveWork must be conservative true with no stores")
	}

	// Convoy manager's lazy retry succeeds.
	ready = true
	if got := d.convoyManager.ensureStoresSnapshot(); len(got) != 1 {
		t.Fatalf("ensureStoresSnapshot = %d stores, want 1", len(got))
	}

	snap := d.collectIdleSnapshot()
	if snap.Err != nil && strings.Contains(snap.Err.Error(), `beads store "hq" unavailable`) {
		t.Fatalf("hq still reported unavailable after lazy open: %v", snap.Err)
	}
	if !d.hasActiveWork() { // sees in_progress bead from the lazily opened store
		t.Fatal("hasActiveWork should see the in_progress bead")
	}
}

func TestStoresReturnsCopyAndEmptyAfterStop(t *testing.T) {
	store := &searchStorage{}
	m := NewConvoyManager("", func(string, ...interface{}) {}, "gt", 0, map[string]beadsdk.Storage{"hq": &closableStore{searchStorage: store}}, nil, nil)
	got := m.Stores()
	delete(got, "hq") // mutating the copy must not affect the owner
	if len(m.Stores()) != 1 {
		t.Fatal("Stores() must return a copy")
	}
	m.Stop()
	if len(m.Stores()) != 0 {
		t.Fatal("Stores() must be empty after Stop")
	}
	m.Stop() // second Stop must not double close
}

type closableStore struct {
	*searchStorage
	mu     sync.Mutex
	closes int
}

func (c *closableStore) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
	return nil
}

func TestStoresConcurrentWithReopenAndStop(t *testing.T) {
	cs := &closableStore{searchStorage: &searchStorage{}}
	opens := 0
	m := NewConvoyManager("", func(string, ...interface{}) {}, "gt", 0, nil, func() map[string]beadsdk.Storage {
		opens++
		return map[string]beadsdk.Storage{"hq": cs}
	}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = m.Stores()
				_ = m.ensureStoresSnapshot()
			}
		}()
	}
	wg.Wait()
	m.Stop()
	m.Stop()
	if cs.closes != 1 {
		t.Fatalf("store closed %d times, want exactly 1", cs.closes)
	}
	if opens != 1 {
		t.Fatalf("opener called %d times, want 1", opens)
	}
}

func TestStoresDoesNotBlockDuringLazyOpen(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	m := NewConvoyManager("", func(string, ...interface{}) {}, "gt", 0, nil, func() map[string]beadsdk.Storage {
		close(entered)
		<-release
		return map[string]beadsdk.Storage{"hq": &searchStorage{}}
	}, nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = m.ensureStoresSnapshot() }()
	<-entered

	got := make(chan int, 1)
	go func() { got <- len(m.Stores()) }()
	select {
	case n := <-got:
		if n != 0 {
			t.Fatalf("Stores() during open = %d, want 0", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stores() blocked while openStores was running")
	}
	close(release)
	<-done
	if len(m.Stores()) != 1 {
		t.Fatal("Stores() should see store after open completes")
	}
}
