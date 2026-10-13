package testutil

import "os"

// ryukReconnectionTimeoutEnv is how long the testcontainers reaper (Ryuk)
// waits after its last client disconnects before removing the session's
// containers.
const ryukReconnectionTimeoutEnv = "RYUK_RECONNECTION_TIMEOUT"

// testRyukReconnectionTimeout outlasts the longest single package run.
const testRyukReconnectionTimeout = "30m"

// All packages in one `go test ./...` share a testcontainers session, and so
// one Ryuk. With the default 10s timeout, Ryuk removed the Dolt containers of
// packages that were still running ~10s after another package exited
// (observed: three containers SIGKILLed 11s after internal/convoy finished,
// then "connection refused" across internal/cmd). Containers are still
// terminated explicitly by TestMain; Ryuk remains the safety net for aborted
// runs, just with a longer grace period.
func init() {
	if os.Getenv(ryukReconnectionTimeoutEnv) != "" || os.Getenv("TESTCONTAINERS_"+ryukReconnectionTimeoutEnv) != "" {
		return
	}
	_ = os.Setenv(ryukReconnectionTimeoutEnv, testRyukReconnectionTimeout)
}
