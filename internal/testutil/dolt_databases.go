//go:build !windows

package testutil

import (
	"database/sql"
	"fmt"
	"testing"
)

// ResetDoltDatabases drops the databases bd creates for each prefix
// (<prefix> and beads_<prefix>) on the shared Dolt test container, now and
// again when the test ends. Tests that `bd init --prefix <p>` against the
// shared container otherwise reuse the previous test's database, which bd
// >= 1.3 rejects with PROJECT IDENTITY MISMATCH (each init writes a new
// project ID). It only ever talks to the test container: if no container is
// running it does nothing, so it can never touch a production server.
func ResetDoltDatabases(t testing.TB, prefixes ...string) {
	t.Helper()
	if err := dropContainerDatabases(prefixes); err != nil {
		t.Fatalf("resetting Dolt test databases %v: %v", prefixes, err)
	}
	t.Cleanup(func() {
		if err := dropContainerDatabases(prefixes); err != nil {
			t.Logf("cleanup: dropping Dolt test databases %v: %v", prefixes, err)
		}
	})
}

func dropContainerDatabases(prefixes []string) error {
	port := DoltContainerPort()
	if port == "" {
		return nil // No test container: never fall back to another server.
	}
	db, err := sql.Open("mysql", fmt.Sprintf("root@tcp(127.0.0.1:%s)/", port))
	if err != nil {
		return err
	}
	defer db.Close()
	for _, prefix := range prefixes {
		for _, name := range []string{prefix, "beads_" + prefix} {
			if _, err := db.Exec("DROP DATABASE IF EXISTS `" + name + "`"); err != nil {
				return fmt.Errorf("drop %s: %w", name, err)
			}
		}
	}
	if _, err := db.Exec("CALL dolt_purge_dropped_databases()"); err != nil {
		return fmt.Errorf("purge dropped databases: %w", err)
	}
	return nil
}
