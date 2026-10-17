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
