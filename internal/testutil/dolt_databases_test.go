//go:build !windows

package testutil

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestResetDoltDatabases_DropsPrefixDatabases(t *testing.T) {
	RequireDoltContainer(t)
	db, err := sql.Open("mysql", fmt.Sprintf("root@tcp(127.0.0.1:%s)/", DoltContainerPort()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"rdtest", "beads_rdtest"} {
		if _, err := db.Exec("CREATE DATABASE IF NOT EXISTS `" + name + "`"); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	ResetDoltDatabases(t, "rdtest")

	for _, name := range []string{"rdtest", "beads_rdtest"} {
		var got string
		err := db.QueryRow("SELECT schema_name FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&got)
		if err != sql.ErrNoRows {
			t.Errorf("database %s still exists (err=%v)", name, err)
		}
	}
}
