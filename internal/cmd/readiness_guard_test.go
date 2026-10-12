package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestCheckReadiness(t *testing.T) {
	ready := &beadInfo{IssueType: "task", Description: "do the thing", AcceptanceCriteria: "- [ ] works"}
	noAC := &beadInfo{IssueType: "feature", Description: "do the thing"}
	empty := &beadInfo{IssueType: "bug"}
	epic := &beadInfo{IssueType: "epic"}
	gate := &beadInfo{IssueType: "gate"}
	chore := &beadInfo{IssueType: "chore"}

	tests := []struct {
		name         string
		mode         config.ReadinessMode
		info         *beadInfo
		readErr      error
		force        bool
		reason       string
		wantAllow    bool
		wantOutcome  string
		wantProblems []string
		wantErr      bool
	}{
		{name: "off ready", mode: config.ReadinessOff, info: ready, wantAllow: true},
		{name: "off unready no event", mode: config.ReadinessOff, info: noAC, wantAllow: true},
		{name: "off read error", mode: config.ReadinessOff, readErr: errors.New("boom"), wantAllow: true},
		{name: "warn ready", mode: config.ReadinessWarn, info: ready, wantAllow: true},
		{name: "warn unready", mode: config.ReadinessWarn, info: noAC, wantAllow: true, wantOutcome: "warned", wantProblems: []string{"missing_acceptance_criteria"}},
		{name: "warn both problems", mode: config.ReadinessWarn, info: empty, wantAllow: true, wantOutcome: "warned", wantProblems: []string{"missing_description", "missing_acceptance_criteria"}},
		{name: "warn read error", mode: config.ReadinessWarn, readErr: errors.New("boom"), wantAllow: true, wantOutcome: "warned", wantProblems: []string{"read_error"}},
		{name: "enforce ready", mode: config.ReadinessEnforce, info: ready, wantAllow: true},
		{name: "enforce unready rejected", mode: config.ReadinessEnforce, info: noAC, wantOutcome: "rejected", wantProblems: []string{"missing_acceptance_criteria"}},
		{name: "enforce force with reason overridden", mode: config.ReadinessEnforce, info: noAC, force: true, reason: "hotfix", wantAllow: true, wantOutcome: "overridden", wantProblems: []string{"missing_acceptance_criteria"}},
		{name: "enforce read error rejected", mode: config.ReadinessEnforce, readErr: errors.New("boom"), wantOutcome: "rejected", wantProblems: []string{"read_error"}},
		{name: "enforce read error nil info rejected", mode: config.ReadinessEnforce, info: nil, readErr: errors.New("boom"), wantOutcome: "rejected", wantProblems: []string{"read_error"}},
		{name: "enforce read error overridden", mode: config.ReadinessEnforce, readErr: errors.New("boom"), force: true, reason: "hotfix", wantAllow: true, wantOutcome: "overridden", wantProblems: []string{"read_error"}},
		{name: "enforce force on ready bead is no-op", mode: config.ReadinessEnforce, info: ready, force: true, reason: "x", wantAllow: true},
		{name: "force without reason errors in off", mode: config.ReadinessOff, info: ready, force: true, wantErr: true},
		{name: "force without reason errors in warn", mode: config.ReadinessWarn, info: noAC, force: true, wantErr: true},
		{name: "force without reason errors in enforce", mode: config.ReadinessEnforce, info: noAC, force: true, wantErr: true},
		{name: "enforce epic not checked", mode: config.ReadinessEnforce, info: epic, wantAllow: true},
		{name: "enforce gate not checked", mode: config.ReadinessEnforce, info: gate, wantAllow: true},
		{name: "enforce chore not checked", mode: config.ReadinessEnforce, info: chore, wantAllow: true},
		{name: "warn epic no event", mode: config.ReadinessWarn, info: epic, wantAllow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkReadiness(tt.mode, tt.info, tt.readErr, tt.force, tt.reason)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "--reason") {
					t.Fatalf("want error mentioning --reason, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Allow != tt.wantAllow {
				t.Errorf("Allow = %v, want %v", got.Allow, tt.wantAllow)
			}
			if got.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %q, want %q", got.Outcome, tt.wantOutcome)
			}
			if !reflect.DeepEqual(got.Problems, tt.wantProblems) {
				t.Errorf("Problems = %v, want %v", got.Problems, tt.wantProblems)
			}
		})
	}
}

func TestReadinessRigFor(t *testing.T) {
	tests := []struct{ target, bead, want string }{
		{"be", "pro", "be"},
		{"", "pro", "pro"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := readinessRigFor(tt.target, tt.bead); got != tt.want {
			t.Errorf("readinessRigFor(%q, %q) = %q, want %q", tt.target, tt.bead, got, tt.want)
		}
	}
}
