package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/gastown/internal/tmux"
)

var isolatedTmuxSocketSeq atomic.Int64

// useIsolatedTmuxSocket points the package default tmux socket at a fresh
// per-test server and restores the previous default on cleanup. Without it,
// tests that create real sessions land on whatever socket an earlier test left
// behind (InitRegistry derives one from a town path, e.g. "internal-d38a64")
// or on the user's own tmux server. The gt-test- prefix lets findTestSockets
// sweep the socket if cleanup never runs. The default socket is process-global,
// so callers must not use t.Parallel.
func useIsolatedTmuxSocket(t *testing.T) *tmux.Tmux {
	t.Helper()
	socket := fmt.Sprintf("gt-test-cmd-%d-%d", os.Getpid(), isolatedTmuxSocketSeq.Add(1))
	old := tmux.GetDefaultSocket()
	tmux.SetDefaultSocket(socket)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		_ = os.Remove(filepath.Join(tmux.SocketDir(), socket))
		tmux.SetDefaultSocket(old)
	})
	return tmux.NewTmux()
}

func TestUseIsolatedTmuxSocket_ScopesAndRestoresDefault(t *testing.T) {
	const leaked = "internal-d38a64"
	orig := tmux.GetDefaultSocket()
	tmux.SetDefaultSocket(leaked)
	t.Cleanup(func() { tmux.SetDefaultSocket(orig) })

	t.Run("isolated", func(t *testing.T) {
		tm := useIsolatedTmuxSocket(t)
		inside := tmux.GetDefaultSocket()
		if inside == leaked || !strings.HasPrefix(inside, "gt-test-") {
			t.Fatalf("default socket = %q, want a fresh gt-test-* socket", inside)
		}
		if tm == nil {
			t.Fatal("useIsolatedTmuxSocket returned nil Tmux")
		}
	})

	if got := tmux.GetDefaultSocket(); got != leaked {
		t.Errorf("default socket after cleanup = %q, want %q restored", got, leaked)
	}
}
