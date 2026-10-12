package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRigSettings_Readiness(t *testing.T) {
	for _, mode := range []string{"", "off", "warn", "enforce"} {
		if err := validateRigSettings(&RigSettings{Type: "rig-settings", Readiness: mode}); err != nil {
			t.Errorf("readiness %q: unexpected error %v", mode, err)
		}
	}
	if err := validateRigSettings(&RigSettings{Type: "rig-settings", Readiness: "strict"}); err == nil {
		t.Error("readiness \"strict\": expected validation error")
	}
}

func TestGetReadinessMode(t *testing.T) {
	rigPath := t.TempDir()
	if got := GetReadinessMode(rigPath); got != ReadinessWarn {
		t.Errorf("missing settings: got %q, want warn", got)
	}

	write := func(body string) {
		t.Helper()
		path := RigSettingsPath(rigPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"type":"rig-settings","version":1}`)
	if got := GetReadinessMode(rigPath); got != ReadinessWarn {
		t.Errorf("unset field: got %q, want warn", got)
	}
	write(`{"type":"rig-settings","version":1,"readiness":"enforce"}`)
	if got := GetReadinessMode(rigPath); got != ReadinessEnforce {
		t.Errorf("enforce: got %q", got)
	}
	write(`{"type":"rig-settings","version":1,"readiness":"off"}`)
	if got := GetReadinessMode(rigPath); got != ReadinessOff {
		t.Errorf("off: got %q", got)
	}
}
