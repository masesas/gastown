package polecat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHeartbeatMatchesRun(t *testing.T) {
	tests := []struct {
		name string
		hb   *SessionHeartbeat
		run  string
		want bool
	}{
		{name: "nil heartbeat", hb: nil, run: "run-a", want: true},
		{name: "legacy heartbeat without generation", hb: &SessionHeartbeat{}, run: "run-a", want: true},
		{name: "session without GT_RUN", hb: &SessionHeartbeat{Generation: "run-a"}, run: "", want: true},
		{name: "same generation", hb: &SessionHeartbeat{Generation: "run-a"}, run: "run-a", want: true},
		{name: "other generation", hb: &SessionHeartbeat{Generation: "run-old"}, run: "run-new", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HeartbeatMatchesRun(tt.hb, tt.run); got != tt.want {
				t.Errorf("HeartbeatMatchesRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadSessionHeartbeatE_MissingVsCorrupt(t *testing.T) {
	townRoot := t.TempDir()

	hb, err := ReadSessionHeartbeatE(townRoot, "gt-missing")
	if err != nil || hb != nil {
		t.Fatalf("missing file: got (%v, %v), want (nil, nil)", hb, err)
	}

	path := heartbeatFile(townRoot, "gt-corrupt")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	hb, err = ReadSessionHeartbeatE(townRoot, "gt-corrupt")
	if err == nil || hb != nil {
		t.Fatalf("corrupt file: got (%v, %v), want (nil, error)", hb, err)
	}
	// Legacy wrapper keeps returning nil for both cases.
	if ReadSessionHeartbeat(townRoot, "gt-corrupt") != nil {
		t.Error("ReadSessionHeartbeat on corrupt file should stay nil for compatibility")
	}
}

func TestTouchSessionHeartbeat_RecordsGenerationFromEnv(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv(EnvRunID, "run-123")

	TouchSessionHeartbeatWithState(townRoot, "gt-gen", HeartbeatExiting, "done", "gt-abc")

	hb, err := ReadSessionHeartbeatE(townRoot, "gt-gen")
	if err != nil || hb == nil {
		t.Fatalf("read heartbeat: (%v, %v)", hb, err)
	}
	if hb.Generation != "run-123" {
		t.Errorf("Generation = %q, want run-123", hb.Generation)
	}
}

func TestTouchSessionHeartbeat_NoRunIDLeavesGenerationEmpty(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv(EnvRunID, "")

	TouchSessionHeartbeat(townRoot, "gt-legacy")

	hb := ReadSessionHeartbeat(townRoot, "gt-legacy")
	if hb == nil {
		t.Fatal("expected heartbeat")
	}
	if hb.Generation != "" {
		t.Errorf("Generation = %q, want empty", hb.Generation)
	}
}

func TestWriteSessionHeartbeat_ReplacesPreviousGeneration(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv(EnvRunID, "run-old")
	TouchSessionHeartbeatWithState(townRoot, "gt-restart", HeartbeatExiting, "", "")

	err := WriteSessionHeartbeat(townRoot, "gt-restart", SessionHeartbeat{
		Timestamp:  time.Now().UTC(),
		State:      HeartbeatWorking,
		Generation: "run-new",
	})
	if err != nil {
		t.Fatalf("WriteSessionHeartbeat: %v", err)
	}

	hb := ReadSessionHeartbeat(townRoot, "gt-restart")
	if hb == nil || hb.Generation != "run-new" || hb.State != HeartbeatWorking {
		t.Fatalf("heartbeat = %+v, want generation run-new state working", hb)
	}
	// No temp files left behind by the atomic write.
	entries, err := os.ReadDir(heartbeatsDir(townRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("heartbeat dir has %d entries, want 1", len(entries))
	}
}

func TestClaimSessionHeartbeat_OverridesCallerRunID(t *testing.T) {
	townRoot := t.TempDir()
	// The caller (e.g., gt sling from the mayor) has its own GT_RUN; the
	// claimed heartbeat must carry the new session's run ID instead.
	t.Setenv(EnvRunID, "caller-run")
	TouchSessionHeartbeatWithState(townRoot, "gt-claim", HeartbeatExiting, "", "")

	claimSessionHeartbeat(townRoot, "gt-claim", "session-run")

	hb := ReadSessionHeartbeat(townRoot, "gt-claim")
	if hb == nil {
		t.Fatal("expected heartbeat")
	}
	if hb.Generation != "session-run" || hb.State != HeartbeatWorking {
		t.Errorf("heartbeat = %+v, want generation session-run state working", hb)
	}
}
