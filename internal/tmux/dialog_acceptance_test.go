package tmux

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAcceptWorkspaceTrustDialog_NoDialog verifies that when no trust dialog
// is present (agent prompt visible), the function returns quickly without error.
func TestAcceptWorkspaceTrustDialog_NoDialog(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-trust-nodlg-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	// Session starts with a shell prompt containing ">", "$", or "%"
	// The polling loop should exit early when it sees the prompt.
	start := time.Now()
	err := tm.AcceptWorkspaceTrustDialog(sessionName)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}

	// Should complete well before the 8s timeout since prompt is visible
	if elapsed > 6*time.Second {
		t.Errorf("took %v, expected early exit (< 6s)", elapsed)
	}
}

// TestAcceptWorkspaceTrustDialog_DetectsCodexDialog verifies that Codex's
// workspace trust prompt is treated as a trust dialog instead of an agent prompt.
func TestAcceptWorkspaceTrustDialog_DetectsCodexDialog(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-trust-codex-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	if err := tm.SendKeys(sessionName, "echo '> You are in /tmp/demo'; echo 'Do you trust the contents of this directory?'"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if err := tm.AcceptWorkspaceTrustDialog(sessionName); err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}
}

// TestAcceptBypassPermissionsWarning_NoDialog verifies that when no bypass
// permissions dialog is present, the function returns quickly without error.
func TestAcceptBypassPermissionsWarning_NoDialog(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-bypass-nodlg-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	start := time.Now()
	err := tm.AcceptBypassPermissionsWarning(sessionName)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AcceptBypassPermissionsWarning: %v", err)
	}

	if elapsed > 6*time.Second {
		t.Errorf("took %v, expected early exit (< 6s)", elapsed)
	}
}

// TestAcceptStartupDialogs_NoDialogs verifies the combined function returns
// quickly when no dialogs are present.
func TestAcceptStartupDialogs_NoDialogs(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-startup-nodlg-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	start := time.Now()
	err := tm.AcceptStartupDialogs(sessionName)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AcceptStartupDialogs: %v", err)
	}

	// Both dialog checks should early-exit when prompt is visible
	if elapsed > 12*time.Second {
		t.Errorf("took %v, expected faster completion", elapsed)
	}
}

// TestAcceptWorkspaceTrustDialog_InvalidSession verifies error handling
// when the session doesn't exist.
func TestAcceptWorkspaceTrustDialog_InvalidSession(t *testing.T) {
	tm := newTestTmux(t)

	// Should not panic or hang — should return nil after timeout
	err := tm.AcceptWorkspaceTrustDialog("gt-nonexistent-session-xyz")
	// CapturePane errors are retried until timeout, then returns nil
	if err != nil {
		t.Fatalf("expected nil error for nonexistent session, got: %v", err)
	}
}

// TestContainsPromptIndicator verifies the prompt detection helper
// recognizes various shell and agent prompt patterns.
func TestContainsPromptIndicator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"claude prompt", "Hello! How can I help?\n>", true},
		{"codex prompt", "Ready\n› ", true},
		{"bash prompt", "user@host:~$", true},
		{"zsh prompt", "╰─❯", true},
		{"root prompt", "root@host:~#", true},
		{"csh prompt", "host%", true},
		{"dialog text only", "Quick safety check\nDo you trust this folder?", false},
		{"empty", "", false},
		{"whitespace only", "   \n  \n  ", false},
		{"bypass dialog", "Bypass Permissions mode\n1. No\n2. Yes, I accept", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsPromptIndicator(tt.content)
			if got != tt.want {
				t.Errorf("containsPromptIndicator(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestContainsWorkspaceTrustDialog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"claude trust prompt", "Quick safety check\nDo you trust this folder?", true},
		{"codex trust prompt", "> You are in /tmp/demo\nDo you trust the contents of this directory?", true},
		{"bypass dialog", "Bypass Permissions mode\n1. No\n2. Yes, I accept", false},
		{"shell prompt", "user@host:~$", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsWorkspaceTrustDialog(tt.content)
			if got != tt.want {
				t.Errorf("containsWorkspaceTrustDialog(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestContainsBlockingStartupDialog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		wantBlocked bool
		wantName    string
	}{
		{
			name: "codex update modal",
			content: `Update available! 0.137.0 -> 0.138.0
Update now
Skip
Skip until next version`,
			wantBlocked: true,
			wantName:    "codex update prompt",
		},
		{
			name:        "codex trust modal",
			content:     "> You are in /tmp/demo\nDo you trust the contents of this directory?",
			wantBlocked: true,
			wantName:    "workspace trust prompt",
		},
		{
			name:        "bypass modal",
			content:     "Bypass Permissions mode\n1. No\n2. Yes, I accept",
			wantBlocked: true,
			wantName:    "bypass permissions prompt",
		},
		{
			name:        "ready prompt",
			content:     "› ",
			wantBlocked: false,
		},
		{
			name: "stale bypass dialog before codex prompt",
			content: `Bypass Permissions mode
1. No
2. Yes, I accept
› `,
			wantBlocked: false,
		},
		{
			name: "stale bypass dialog before prompt and status",
			content: `Bypass Permissions mode
1. No
2. Yes, I accept
›
session ready`,
			wantBlocked: false,
		},
		{
			name: "stale trust dialog before shell prompt",
			content: `Quick safety check
Do you trust this folder?
user@host:~$`,
			wantBlocked: false,
		},
		{
			name: "old shell prompt before current bypass dialog",
			content: `user@host:~$
Bypass Permissions mode
1. No
2. Yes, I accept`,
			wantBlocked: true,
			wantName:    "bypass permissions prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotBlocked := containsBlockingStartupDialog(tt.content)
			if gotBlocked != tt.wantBlocked {
				t.Fatalf("blocked = %v, want %v", gotBlocked, tt.wantBlocked)
			}
			if gotName != tt.wantName {
				t.Fatalf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

// fakeSelectDialog is a bash stand-in for Claude Code's select dialog, matching
// what v2.1.296 does in a real pane: the cursor starts on the given option,
// Down/Up wrap around, digit keys are ignored (hideIndexes), and the first
// dropKeys keystrokes are swallowed (refuseInput right after the dialog opens).
// Enter writes the highlighted label to the result file and clears the dialog.
const fakeSelectDialog = `result=$1 dropKeys=$2 cur=$3 mark=$4 title=$5; shift 5; opts=("$@")
render() {
  printf '\033[2J\033[H %s\n' "$title"
  for i in "${!opts[@]}"; do
    if [ "$i" -eq "$cur" ]; then printf ' %s %s\n' "$mark" "${opts[$i]}"; else printf '   %s\n' "${opts[$i]}"; fi
  done
  printf ' Enter to confirm · Esc to cancel\n'
}
render
seen=0
while IFS= read -rsn1 k; do
  rest=''
  if [ "$k" = $'\e' ]; then IFS= read -rsn2 -t 1 rest; fi
  seen=$((seen + 1))
  [ "$seen" -le "$dropKeys" ] && continue
  case "$k$rest" in
    $'\e[B'|$'\eOB') cur=$(( (cur + 1) % ${#opts[@]} )); render ;;
    $'\e[A'|$'\eOA') cur=$(( (cur + ${#opts[@]} - 1) % ${#opts[@]} )); render ;;
    '') printf '%s' "${opts[$cur]}" > "$result"; printf '\033[2J\033[H dialog closed\n'; exec sleep 600 ;;
  esac
done
`

type fakeDialog struct {
	title    string
	options  []string
	cursor   int
	mark     string // cursor glyph: Claude "❯", Codex "›"
	dropKeys int
}

// startFakeDialog runs fakeSelectDialog in a new session on the isolated test
// socket and returns the session name plus a func reading the confirmed label
// ("" when nothing was confirmed).
func startFakeDialog(t *testing.T, tm *Tmux, d fakeDialog) (string, func() string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "dialog.sh")
	result := filepath.Join(dir, "result")
	if err := os.WriteFile(script, []byte(fakeSelectDialog), 0o600); err != nil {
		t.Fatalf("write fake dialog: %v", err)
	}
	if d.mark == "" {
		d.mark = "❯"
	}
	args := []string{"bash", script, result, strconv.Itoa(d.dropKeys), strconv.Itoa(d.cursor), d.mark, d.title}
	args = append(args, d.options...)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}

	name := "gt-test-dlg-" + strings.ReplaceAll(t.Name(), "/", "-")
	_ = tm.KillSession(name)
	if err := tm.NewSessionWithCommand(name, dir, strings.Join(quoted, " ")); err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	t.Cleanup(func() { _ = tm.KillSession(name) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		content, _ := tm.CapturePane(name, 30)
		if strings.Contains(content, "Enter to confirm") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake dialog never rendered; pane:\n%s", content)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return name, func() string {
		b, err := os.ReadFile(result)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

const fakeTrustTitle = "Quick safety check: Is this a project you created or one you trust?"

// TestAcceptWorkspaceTrustDialog_ClaudeFocusesNoExit is the gt-swqb regression:
// Claude 2.1.296 focuses "No, exit", so a bare Enter quit the agent.
func TestAcceptWorkspaceTrustDialog_ClaudeFocusesNoExit(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   fakeTrustTitle,
		options: []string{"No, exit", "Yes, I trust this folder"},
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}
	if got := selected(); got != "Yes, I trust this folder" {
		t.Fatalf("confirmed %q, want %q", got, "Yes, I trust this folder")
	}
}

// TestAcceptWorkspaceTrustDialog_ClaudeTrustAlreadyFocused covers older Claude
// layouts where the trust option is first and focused: Down would wrap past it.
func TestAcceptWorkspaceTrustDialog_ClaudeTrustAlreadyFocused(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   fakeTrustTitle,
		options: []string{"1. Yes, I trust this folder", "2. No, exit"},
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}
	if got := selected(); got != "1. Yes, I trust this folder" {
		t.Fatalf("confirmed %q, want the trust option", got)
	}
}

// TestAcceptWorkspaceTrustDialog_ClaudeDropsEarlyKeys verifies the cursor is
// re-read after every key: Claude ignores input right after the dialog opens.
func TestAcceptWorkspaceTrustDialog_ClaudeDropsEarlyKeys(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:    fakeTrustTitle,
		options:  []string{"No, exit", "Yes, I trust this folder"},
		dropKeys: 2,
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}
	if got := selected(); got != "Yes, I trust this folder" {
		t.Fatalf("confirmed %q, want %q", got, "Yes, I trust this folder")
	}
}

// TestAcceptWorkspaceTrustDialog_FailsClosedWhenStuck verifies an error (not a
// silent nil) when the dialog never reaches the trust option.
func TestAcceptWorkspaceTrustDialog_FailsClosedWhenStuck(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:    fakeTrustTitle,
		options:  []string{"No, exit", "Yes, I trust this folder"},
		dropKeys: 1000,
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err == nil {
		t.Fatal("expected error while trust dialog stays on screen, got nil")
	}
	if got := selected(); got != "" {
		t.Fatalf("confirmed %q, want nothing confirmed", got)
	}
}

// TestAcceptWorkspaceTrustDialog_UnknownClaudeOptionsFailClosed verifies no key
// is guessed when the trust option label is not on screen.
func TestAcceptWorkspaceTrustDialog_UnknownClaudeOptionsFailClosed(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   fakeTrustTitle,
		options: []string{"No, exit", "Proceed anyway"},
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err == nil {
		t.Fatal("expected error for unrecognized trust options, got nil")
	}
	if got := selected(); got != "" {
		t.Fatalf("confirmed %q, want nothing confirmed", got)
	}
}

// TestAcceptWorkspaceTrustDialog_CodexPreselected keeps Codex working: its
// "Yes, continue" option is focused by default, so Enter alone accepts it.
func TestAcceptWorkspaceTrustDialog_CodexPreselected(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   "Do you trust the contents of this directory?",
		options: []string{"1. Yes, continue", "2. No, quit"},
		mark:    "›",
	})

	if err := tm.AcceptWorkspaceTrustDialog(session); err != nil {
		t.Fatalf("AcceptWorkspaceTrustDialog: %v", err)
	}
	if got := selected(); got != "1. Yes, continue" {
		t.Fatalf("confirmed %q, want %q", got, "1. Yes, continue")
	}
}

const fakeBypassTitle = "WARNING: Claude Code running in Bypass Permissions mode"

// TestAcceptBypassPermissionsWarning_ClaudeFocusesNoExit covers the real
// layout: same select component as the trust dialog, "No, exit" focused.
func TestAcceptBypassPermissionsWarning_ClaudeFocusesNoExit(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   fakeBypassTitle,
		options: []string{"No, exit", "Yes, I accept"},
	})

	if err := tm.AcceptBypassPermissionsWarning(session); err != nil {
		t.Fatalf("AcceptBypassPermissionsWarning: %v", err)
	}
	if got := selected(); got != "Yes, I accept" {
		t.Fatalf("confirmed %q, want %q", got, "Yes, I accept")
	}
}

// TestAcceptBypassPermissionsWarning_AcceptAlreadyFocused guards the old blind
// Down+Enter, which wrapped onto "No, exit" when "Yes, I accept" was focused.
func TestAcceptBypassPermissionsWarning_AcceptAlreadyFocused(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:   fakeBypassTitle,
		options: []string{"No, exit", "Yes, I accept"},
		cursor:  1,
	})

	if err := tm.AcceptBypassPermissionsWarning(session); err != nil {
		t.Fatalf("AcceptBypassPermissionsWarning: %v", err)
	}
	if got := selected(); got != "Yes, I accept" {
		t.Fatalf("confirmed %q, want %q", got, "Yes, I accept")
	}
}

// TestAcceptBypassPermissionsWarning_RetriesDroppedEnter covers a dropped
// Enter while the wanted option is already focused (refuseInput window).
func TestAcceptBypassPermissionsWarning_RetriesDroppedEnter(t *testing.T) {
	tm := newTestTmux(t)
	session, selected := startFakeDialog(t, tm, fakeDialog{
		title:    fakeBypassTitle,
		options:  []string{"No, exit", "Yes, I accept"},
		cursor:   1,
		dropKeys: 1,
	})

	if err := tm.AcceptBypassPermissionsWarning(session); err != nil {
		t.Fatalf("AcceptBypassPermissionsWarning: %v", err)
	}
	if got := selected(); got != "Yes, I accept" {
		t.Fatalf("confirmed %q, want %q", got, "Yes, I accept")
	}
}
