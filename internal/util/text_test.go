package util

import "testing"

func TestOneLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"folds whitespace", "a\nb\r\n\tc  d", 0, "a b c d"},
		{"redacts URL credentials", "fetch https://user:tok@host/x failed", 0, "fetch https://***@host/x failed"},
		{"truncates on a rune boundary", "ééé", 3, "é…"},
		{"short text unchanged", "ok", 10, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OneLine(tt.in, tt.max); got != tt.want {
				t.Fatalf("OneLine(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestHasControlChars(t *testing.T) {
	for in, want := range map[string]bool{"plain text": false, "tab\there": true, "esc\x1b[31m": true, "nul\x00": true, "é ok": false, "line\nbreak": true} {
		if got := HasControlChars(in); got != want {
			t.Errorf("HasControlChars(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOneLineDropsControlCharacters(t *testing.T) {
	for in, want := range map[string]string{
		"ok\x1b[31mred\x1b[0m":   "ok[31mred[0m",
		"nul\x00byte":            "nulbyte",
		"bell\x07 and\r\nbreak":  "bell and break",
		"del\x7fchar":            "delchar",
		"c1\u0085next\u009bline": "c1 nextline",
	} {
		if got := OneLine(in, 0); got != want || HasControlChars(got) {
			t.Errorf("OneLine(%q) = %q, want %q (no control characters)", in, got, want)
		}
	}
}

func TestTruncateUTF8(t *testing.T) {
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly", 7, "exactly"},
		{"abcdef", 3, "abc…"},
		{"ééé", 3, "é…"}, // é is 2 bytes: never cut inside a rune
		{"日本語", 4, "日…"},
		{"anything", 0, "anything"},
	} {
		if got := TruncateUTF8(tc.in, tc.max); got != tc.want {
			t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}
