package testutil

import "testing"

// storeUnavailableIsFailure reports whether a beads store that cannot be
// opened should fail the test rather than skip it: once this process has a
// Dolt test container, an unreachable store means the container died or the
// test is broken, and skipping would hide that (previously a dying container
// silently turned store tests into skips).
func storeUnavailableIsFailure() bool {
	return DoltContainerPort() != ""
}

// StoreUnavailable reports a beads store that could not be opened: it fails
// the test when a Dolt test container is running in this process and skips
// it otherwise (no Docker, no CGO/Dolt).
func StoreUnavailable(t testing.TB, format string, args ...any) {
	t.Helper()
	if storeUnavailableIsFailure() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}
