package util

import "testing"

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no credentials", "https://github.com/org/repo", "https://github.com/org/repo"},
		{"user info", "https://user:pass@github.com/org/repo", "https://github.com/org/repo"},
		{"token auth", "https://x-access-token:ghp_abc123@github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"user only", "https://user@github.com/org/repo", "https://github.com/org/repo"},
		{"ssh url", "git@github.com:org/repo.git", "git@github.com:org/repo.git"},
		{"empty string", "", ""},
		{"invalid url no creds", "://bad", "://bad"},
		{"invalid url with creds", "://user:pass@host", "<invalid URL>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.in)
			if got != tt.want {
				t.Errorf("RedactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactURLCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"fatal: unable to access 'https://x-access-token:ghp_abc@github.com/o/r.git/': 403": "fatal: unable to access 'https://***@github.com/o/r.git/': 403",
		"fetch https://alice@host/repo and ssh://bob:pw@h2/x":                               "fetch https://***@host/repo and ssh://***@h2/x",
		"git@github.com:org/repo.git not found":                                             "git@github.com:org/repo.git not found",
		"https://github.com/o/r has no creds":                                               "https://github.com/o/r has no creds",
	} {
		if got := RedactURLCredentials(in); got != want {
			t.Errorf("RedactURLCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}
