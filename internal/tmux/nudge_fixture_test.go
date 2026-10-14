package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pane captures from Claude Code 2.1.296 (80x24), recorded live while
// reproducing gt-dzck. In this version the status bar does not render
// "esc to interrupt" while a turn or a foreground Bash tool is running, and an
// Escape keystroke in that state aborts the tool exactly like a user rejection.
func readClaudeFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.296", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}

// gt-dzck: a nudge must never send Escape to a pane unless the composer is
// visibly in vim INSERT mode — the only state the Escape exists for (GH#307).
// Live repro showed Escape in INSERT mode is consumed by vim even while a tool
// runs, while Escape anywhere else interrupts the running tool or cancels the
// open dialog.
func TestShouldSendEscapeForLines_Claude2_1_296Fixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    bool
	}{
		{"idle.txt", false},
		{"busy-generating.txt", false},
		{"busy-bash.txt", false},
		{"busy-bash-accept-edits.txt", false},
		{"busy-bash-queued.txt", false},
		{"askuserquestion.txt", false},
		{"rewind.txt", false},
		{"idle-vim-insert.txt", true},
		{"busy-bash-vim-insert.txt", true},
		// Tool output quoting the indicator is not the status bar.
		{"busy-bash-output-mentions-markers.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			lines := strings.Split(readClaudeFixture(t, tt.fixture), "\n")
			if got := shouldSendEscapeForLines(lines); got != tt.want {
				t.Errorf("shouldSendEscapeForLines(%s) = %v, want %v", tt.fixture, got, tt.want)
			}
		})
	}
}

// gt-dzck: only the real Rewind menu may trigger the pre-delivery Escape.
// AskUserQuestion ("Enter to select · ↑/↓ to navigate · Esc to cancel") used to
// match, so a nudge cancelled the question ("User declined to answer").
func TestContainsRewindIndicators_Claude2_1_296Fixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    bool
	}{
		{"rewind.txt", true},
		{"askuserquestion.txt", false},
		{"idle.txt", false},
		{"busy-bash.txt", false},
		{"busy-bash-queued.txt", false},
		{"idle-vim-insert.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			if got := containsRewindIndicators(readClaudeFixture(t, tt.fixture)); got != tt.want {
				t.Errorf("containsRewindIndicators(%s) = %v, want %v", tt.fixture, got, tt.want)
			}
		})
	}
}

// gt-dzck: with no Escape sent, the nudge's Enter would answer whatever modal
// is open — live repro showed it picking "1. Yes" on a permission prompt and
// the first option of AskUserQuestion. Nudges must not type into modals.
func TestContainsOpenDialog_Claude2_1_296Fixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    bool
	}{
		{"permission.txt", true},
		{"askuserquestion.txt", true},
		{"rewind.txt", true},
		{"idle.txt", false},
		{"idle-vim-insert.txt", false},
		{"busy-generating.txt", false},
		{"busy-bash.txt", false},
		{"busy-bash-accept-edits.txt", false},
		{"busy-bash-queued.txt", false},
		{"busy-bash-vim-insert.txt", false},
		// Tool output quoting dialog footers is not an open dialog.
		{"busy-bash-output-mentions-markers.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			if got := containsOpenDialog(readClaudeFixture(t, tt.fixture)); got != tt.want {
				t.Errorf("containsOpenDialog(%s) = %v, want %v", tt.fixture, got, tt.want)
			}
		})
	}
}
