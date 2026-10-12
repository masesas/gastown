package cmd

import (
	"testing"

	"github.com/steveyegge/gastown/internal/polecat"
)

func TestHeartbeatShowsDoneOrIdle(t *testing.T) {
	tests := []struct {
		name string
		hb   *polecat.SessionHeartbeat
		run  string
		want bool
	}{
		{name: "no heartbeat", hb: nil, run: "run-a", want: false},
		{name: "working", hb: &polecat.SessionHeartbeat{State: polecat.HeartbeatWorking, Generation: "run-a"}, run: "run-a", want: false},
		{name: "exiting same run", hb: &polecat.SessionHeartbeat{State: polecat.HeartbeatExiting, Generation: "run-a"}, run: "run-a", want: true},
		{name: "idle same run", hb: &polecat.SessionHeartbeat{State: polecat.HeartbeatIdle, Generation: "run-a"}, run: "run-a", want: true},
		{name: "exiting previous run", hb: &polecat.SessionHeartbeat{State: polecat.HeartbeatExiting, Generation: "run-old"}, run: "run-new", want: false},
		{name: "legacy exiting", hb: &polecat.SessionHeartbeat{State: polecat.HeartbeatExiting}, run: "run-new", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := heartbeatShowsDoneOrIdle(tt.hb, tt.run); got != tt.want {
				t.Errorf("heartbeatShowsDoneOrIdle() = %v, want %v", got, tt.want)
			}
		})
	}
}
