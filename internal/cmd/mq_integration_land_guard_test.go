package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestCheckIntegrationLandAllowed is the regression test for gt-pd1c.71: a
// refinery agent ran `gt mq integration land` on a rig with
// integration_branch_auto_land=false and merged an epic into main without a
// human. Agents must be refused; humans, dry-runs and auto_land=true rigs keep
// the previous behaviour.
func TestCheckIntegrationLandAllowed(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		autoLand bool
		dryRun   bool
		wantErr  bool
	}{
		{"refinery agent, auto_land=false: refused", map[string]string{"GT_ROLE": "refinery"}, false, false, true},
		{"qualified GT_ROLE, auto_land=false: refused", map[string]string{"GT_ROLE": "posin/refinery"}, false, false, true},
		{"polecat via GT_POLECAT only: refused", map[string]string{"GT_POLECAT": "toast"}, false, false, true},
		{"crew via GT_CREW only: refused", map[string]string{"GT_CREW": "joe"}, false, false, true},
		{"BD_ACTOR only: refused", map[string]string{"BD_ACTOR": "posin/refinery"}, false, false, true},
		{"human, auto_land=false: allowed", map[string]string{}, false, false, false},
		{"human with unrelated git identity env: allowed", map[string]string{"GIT_AUTHOR_NAME": "Jane"}, false, false, false},
		{"whitespace-only agent var is not an identity", map[string]string{"GT_ROLE": "  "}, false, false, false},
		{"agent dry-run, auto_land=false: allowed", map[string]string{"GT_ROLE": "refinery"}, false, true, false},
		{"agent, auto_land=true: allowed (unchanged)", map[string]string{"GT_ROLE": "refinery"}, true, false, false},
		{"human, auto_land=true: allowed (unchanged)", map[string]string{}, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkIntegrationLandAllowed("posin", "kp-4az", tt.autoLand, tt.dryRun, envFrom(tt.env))
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkIntegrationLandAllowed() err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			msg := err.Error()
			want := "auto-land disabled for rig posin; a human must run gt mq integration land kp-4az"
			if !strings.Contains(msg, want) {
				t.Fatalf("error %q does not contain %q", msg, want)
			}
		})
	}
}

// TestCheckIntegrationLandAllowed_ErrorOmitsEnvValues keeps env values out of
// the error text: only the name of the variable that identified the agent is
// reported.
func TestCheckIntegrationLandAllowed_ErrorOmitsEnvValues(t *testing.T) {
	err := checkIntegrationLandAllowed("posin", "kp-4az", false, false,
		envFrom(map[string]string{"BD_ACTOR": "secret-actor-value"}))
	if err == nil {
		t.Fatal("expected refusal")
	}
	if strings.Contains(err.Error(), "secret-actor-value") {
		t.Fatalf("error leaks env value: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "BD_ACTOR") {
		t.Fatalf("error should name the identifying variable: %q", err.Error())
	}
}

// TestIntegrationLandAgentEnvVars_AreIdentityEnvVars ties the agent markers to
// the canonical identity list so the two cannot drift apart.
func TestIntegrationLandAgentEnvVars_AreIdentityEnvVars(t *testing.T) {
	known := make(map[string]bool, len(config.IdentityEnvVars))
	for _, k := range config.IdentityEnvVars {
		known[k] = true
	}
	for _, k := range agentSessionEnvVars {
		if !known[k] {
			t.Errorf("%s is not in config.IdentityEnvVars", k)
		}
	}
}

func TestIntegrationAutoLandEnabled(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		rig := t.TempDir()
		if body == "" {
			return rig
		}
		dir := filepath.Join(rig, "settings")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return rig
	}
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"no settings file: disabled", "", false},
		{"corrupt settings: disabled (fail closed)", "{not json", false},
		{"explicit false", `{"type":"rig-settings","version":1,"merge_queue":{"integration_branch_auto_land":false}}`, false},
		{"explicit true", `{"type":"rig-settings","version":1,"merge_queue":{"integration_branch_auto_land":true}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := integrationAutoLandEnabled(write(t, tt.body)); got != tt.want {
				t.Fatalf("integrationAutoLandEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
