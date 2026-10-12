package testutil

import (
	"os"
	"testing"
)

func TestImportExtendsRyukReconnectionTimeout(t *testing.T) {
	if os.Getenv("TESTCONTAINERS_"+ryukReconnectionTimeoutEnv) != "" {
		t.Skip("caller configured the TESTCONTAINERS_ variant explicitly")
	}
	if got := os.Getenv(ryukReconnectionTimeoutEnv); got == "" || got == "10s" {
		t.Fatalf("%s = %q after importing testutil, want an extended timeout", ryukReconnectionTimeoutEnv, got)
	}
}
