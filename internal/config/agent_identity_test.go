package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentPlusEmail(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, email, tag, want string }{
		{"polecat", "vandikalvandi@gmail.com", "nuka", "vandikalvandi+nuka@gmail.com"},
		{"slash tag", "vandikalvandi@gmail.com", "posin/refinery", "vandikalvandi+posin-refinery@gmail.com"},
		{"dots kept", "a@b.co", "x.y_z-1", "a+x.y_z-1@b.co"},
		{"spaces replaced", "a@b.co", "my agent", "a+my-agent@b.co"},
		{"empty email", "", "nuka", ""},
		{"no at", "nonsense", "nuka", ""},
		{"no local", "@b.co", "nuka", ""},
		{"no domain", "a@", "nuka", ""},
		{"angle brackets", "A <a@b.co>", "nuka", ""},
		{"empty tag", "a@b.co", "", ""},
		{"tag lowercased", "a@b.co", "Posin/Refinery", "a+posin-refinery@b.co"},
		{"dashes collapsed", "a@b.co", "x//--y", "a+x-y@b.co"},
		{"dashes trimmed", "a@b.co", "/x/", "a+x@b.co"},
		{"two at", "a@b@c.co", "n", ""},
		{"domain no dot", "a@localhost", "n", ""},
		{"domain leading dot", "a@.co", "n", ""},
		{"domain trailing dot", "a@b.co.", "n", ""},
		{"local space", "a b@c.co", "n", ""},
		{"local quote", "a\"b@c.co", "n", ""},
		{"local semicolon", "a;b@c.co", "n", ""},
		{"local parens", "a(b)@c.co", "n", ""},
		{"existing plus replaced", "a+old@b.co", "nuka", "a+nuka@b.co"},
		{"empty local before plus", "+old@b.co", "nuka", ""},
		{"all-unsafe tag", "a@b.co", "///", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AgentPlusEmail(tt.email, tt.tag); got != tt.want {
				t.Errorf("AgentPlusEmail(%q,%q) = %q, want %q", tt.email, tt.tag, got, tt.want)
			}
		})
	}
}

func TestGitIdentityEnv(t *testing.T) {
	t.Parallel()
	env := GitIdentityEnv("posin/refinery", "v@g.com")
	want := map[string]string{
		"GIT_AUTHOR_NAME": "posin/refinery", "GIT_COMMITTER_NAME": "posin/refinery",
		"GIT_AUTHOR_EMAIL": "v+posin-refinery@g.com", "GIT_COMMITTER_EMAIL": "v+posin-refinery@g.com",
	}
	for k, v := range want {
		assertEnv(t, env, k, v)
	}
	env = GitIdentityEnv("nuka", "")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "nuka")
	assertEnv(t, env, "GIT_COMMITTER_NAME", "nuka")
	assertNotSet(t, env, "GIT_AUTHOR_EMAIL")
	assertNotSet(t, env, "GIT_COMMITTER_EMAIL")
}

func TestResolveOverseerEmail_FromOverseerJSON(t *testing.T) {
	t.Parallel()
	town := t.TempDir()
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveOverseerConfig(OverseerConfigPath(town), &OverseerConfig{Name: "H", Email: "h@x.io"}); err != nil {
		t.Fatal(err)
	}
	if got := ResolveOverseerEmail(town); got != "h@x.io" {
		t.Errorf("got %q", got)
	}
	if got := ResolveOverseerEmail(""); got != "" {
		t.Errorf("empty town root: got %q", got)
	}
}

func TestAgentEnv_GitIdentityPerRole(t *testing.T) {
	t.Parallel()
	const mail = "v@g.com"
	tests := []struct {
		cfg      AgentEnvConfig
		name, em string
	}{
		{AgentEnvConfig{Role: "mayor"}, "mayor", "v+mayor@g.com"},
		{AgentEnvConfig{Role: "deacon"}, "deacon", "v+deacon@g.com"},
		{AgentEnvConfig{Role: "boot"}, "boot", "v+boot@g.com"},
		{AgentEnvConfig{Role: "witness", Rig: "posin"}, "posin/witness", "v+posin-witness@g.com"},
		{AgentEnvConfig{Role: "refinery", Rig: "posin"}, "posin/refinery", "v+posin-refinery@g.com"},
		{AgentEnvConfig{Role: "polecat", Rig: "posin", AgentName: "nuka"}, "nuka", "v+nuka@g.com"},
		{AgentEnvConfig{Role: "crew", Rig: "posin", AgentName: "dom"}, "dom", "v+dom@g.com"},
		{AgentEnvConfig{Role: "dog", AgentName: "alpha"}, "alpha", "v+alpha@g.com"},
		{AgentEnvConfig{Role: "dog"}, "dog", "v+dog@g.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.cfg.OverseerEmail = mail
			env := AgentEnv(tt.cfg)
			assertEnv(t, env, "GIT_AUTHOR_NAME", tt.name)
			assertEnv(t, env, "GIT_COMMITTER_NAME", tt.name)
			assertEnv(t, env, "GIT_AUTHOR_EMAIL", tt.em)
			assertEnv(t, env, "GIT_COMMITTER_EMAIL", tt.em)
		})
	}
}

func TestAgentEnv_NoOverseerEmailKeepsLegacy(t *testing.T) {
	// No overseer.json and no global git email: legacy name-only behavior.
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	env := AgentEnv(AgentEnvConfig{Role: "polecat", Rig: "r", AgentName: "nuka", TownRoot: "/nonexistent-town"})
	assertEnv(t, env, "GIT_AUTHOR_NAME", "nuka")
	assertNotSet(t, env, "GIT_AUTHOR_EMAIL")
	assertNotSet(t, env, "GIT_COMMITTER_EMAIL")
}

func TestGitIdentityName(t *testing.T) {
	t.Parallel()
	tests := []struct{ role, rig, agent, want string }{
		{"mayor", "", "", "mayor"},
		{"boot", "", "boot", "boot"},
		{"refinery", "posin", "", "posin/refinery"},
		{"polecat", "posin", "nuka", "nuka"},
		{"crew", "posin", "dom", "dom"},
		{"dog", "", "", "dog"},
		{"bogus", "r", "a", ""},
	}
	for _, tt := range tests {
		if got := GitIdentityName(tt.role, tt.rig, tt.agent); got != tt.want {
			t.Errorf("GitIdentityName(%q,%q,%q) = %q, want %q", tt.role, tt.rig, tt.agent, got, tt.want)
		}
	}
}

func TestResolveOverseerEmail_CacheInvalidatesOnMtime(t *testing.T) {
	t.Parallel()
	town := t.TempDir()
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := OverseerConfigPath(town)
	if err := SaveOverseerConfig(path, &OverseerConfig{Name: "H", Email: "one@x.io"}); err != nil {
		t.Fatal(err)
	}
	if got := ResolveOverseerEmail(town); got != "one@x.io" {
		t.Fatalf("got %q", got)
	}
	if err := SaveOverseerConfig(path, &OverseerConfig{Name: "H", Email: "two@x.io"}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if got := ResolveOverseerEmail(town); got != "two@x.io" {
		t.Errorf("after rewrite got %q, want two@x.io", got)
	}
}
