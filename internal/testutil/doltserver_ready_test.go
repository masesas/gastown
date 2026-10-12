//go:build !windows

package testutil

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// The dolt image prints "Server ready" before its entrypoint has created the
// test database and user. Tests released at that point can make the
// entrypoint (set -e) fail and take the whole container down mid-suite, so
// runDoltContainer must only return once the entrypoint has finished.
func TestRunDoltContainer_WaitsForEntrypointInit(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}
	ctx := context.Background()
	ctr, err := runDoltContainerWithRetry(ctx)
	if err != nil {
		t.Fatalf("starting container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })

	logs, err := ctr.Logs(ctx)
	if err != nil {
		t.Fatalf("reading logs: %v", err)
	}
	defer logs.Close()
	data, _ := io.ReadAll(logs)
	if !strings.Contains(string(data), doltEntrypointDoneLog) {
		t.Fatalf("container returned before the entrypoint finished; logs:\n%s", data)
	}
}
