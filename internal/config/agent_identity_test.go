package config

import (
	"os"
	"path/filepath"
	"testing"
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
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{Role: "polecat", Rig: "r", AgentName: "nuka", TownRoot: "/nonexistent-town"})
	assertEnv(t, env, "GIT_AUTHOR_NAME", "nuka")
	assertNotSet(t, env, "GIT_AUTHOR_EMAIL")
	assertNotSet(t, env, "GIT_COMMITTER_EMAIL")
}
