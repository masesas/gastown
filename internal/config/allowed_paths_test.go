package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMatchAllowedPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"apps/**", "apps/mobile/src/main.dart", true},
		{"apps/**", "apps/x", true},
		{"apps/**", "apps", true},
		{"apps/**", ".brief-reviews/kp-4az-r1/mobile-consumer.md", false},
		{"apps/**", "appsx/y", false},
		{"apps/**", "other/apps/y", false},
		{"contracts/openapi.yaml", "contracts/openapi.yaml", true},
		{"contracts/openapi.yaml", "contracts/openapi.yml", false},
		{"contracts/openapi.yaml", "contracts/v2/openapi.yaml", false},
		{"contracts/*.yaml", "contracts/openapi.yaml", true},
		{"contracts/*.yaml", "contracts/v2/openapi.yaml", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/a/b.md", true},
		{"**/*.md", "docs/a/b.go", false},
		{"apps/**/test/*.go", "apps/test/a.go", true},
		{"apps/**/test/*.go", "apps/x/y/test/a.go", true},
		{"apps/**/test/*.go", "apps/x/y/a.go", false},
		{"**", "anything/at/all", true},
		{"docs/?.md", "docs/a.md", true},
		{"docs/?.md", "docs/ab.md", false},
	}
	for _, tt := range tests {
		if got := MatchAllowedPath(tt.pattern, tt.path); got != tt.want {
			t.Errorf("MatchAllowedPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestValidateAllowedPaths(t *testing.T) {
	valid := [][]string{
		nil,
		{},
		{"apps/**", "contracts/openapi.yaml"},
		{"**/*.md", "docs/[a-z]*.md"},
	}
	for _, patterns := range valid {
		if err := ValidateAllowedPaths(patterns); err != nil {
			t.Errorf("ValidateAllowedPaths(%q) = %v, want nil", patterns, err)
		}
	}

	invalid := [][]string{
		{""},
		{"   "},
		{" apps/**"},
		{"/apps/**"},
		{"apps/"},
		{"apps//x"},
		{"./apps/**"},
		{"apps/../secrets"},
		{"apps/a**b"},
		{"apps/[unclosed"},
	}
	for _, patterns := range invalid {
		err := ValidateAllowedPaths(patterns)
		if err == nil {
			t.Errorf("ValidateAllowedPaths(%q) = nil, want error", patterns)
			continue
		}
		if !errors.Is(err, ErrInvalidAllowedPath) {
			t.Errorf("ValidateAllowedPaths(%q) error = %v, want ErrInvalidAllowedPath", patterns, err)
		}
	}
}

func TestMergeQueueConfigPathsOutsideScope(t *testing.T) {
	scoped := &MergeQueueConfig{AllowedPaths: []string{"apps/**", "contracts/openapi.yaml"}}
	changed := []string{
		"apps/mobile/lib/main.dart",
		".brief-reviews/kp-4az-r1/mobile-consumer.md",
		"contracts/openapi.yaml",
		"contracts/other.yaml",
	}
	want := []string{".brief-reviews/kp-4az-r1/mobile-consumer.md", "contracts/other.yaml"}
	if got := scoped.PathsOutsideScope(changed); !reflect.DeepEqual(got, want) {
		t.Errorf("PathsOutsideScope() = %q, want %q", got, want)
	}
	if !scoped.HasPathScope() {
		t.Error("HasPathScope() = false for configured allowed_paths")
	}
	if got := scoped.PathsOutsideScope([]string{"apps/a", "contracts/openapi.yaml"}); got != nil {
		t.Errorf("PathsOutsideScope(in-scope) = %q, want nil", got)
	}

	for name, c := range map[string]*MergeQueueConfig{
		"nil config":    nil,
		"empty list":    {AllowedPaths: []string{}},
		"absent config": {},
	} {
		if c.HasPathScope() {
			t.Errorf("%s: HasPathScope() = true, want false (unrestricted)", name)
		}
		if got := c.PathsOutsideScope(changed); got != nil {
			t.Errorf("%s: PathsOutsideScope() = %q, want nil (unrestricted)", name, got)
		}
	}
}

func TestLoadRigSettings_AllowedPaths(t *testing.T) {
	dir := t.TempDir()
	write := func(t *testing.T, mq map[string]any) string {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"type":        "rig-settings",
			"version":     1,
			"merge_queue": mq,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("valid patterns load", func(t *testing.T) {
		path := write(t, map[string]any{"allowed_paths": []string{"apps/**", "contracts/openapi.yaml"}})
		settings, err := LoadRigSettings(path)
		if err != nil {
			t.Fatalf("LoadRigSettings: %v", err)
		}
		want := []string{"apps/**", "contracts/openapi.yaml"}
		if !reflect.DeepEqual(settings.MergeQueue.AllowedPaths, want) {
			t.Errorf("AllowedPaths = %q, want %q", settings.MergeQueue.AllowedPaths, want)
		}
	})

	t.Run("invalid pattern fails load", func(t *testing.T) {
		path := write(t, map[string]any{"allowed_paths": []string{"/apps/**"}})
		if _, err := LoadRigSettings(path); !errors.Is(err, ErrInvalidAllowedPath) {
			t.Fatalf("LoadRigSettings error = %v, want ErrInvalidAllowedPath", err)
		}
	})

	t.Run("absent key keeps old behaviour", func(t *testing.T) {
		path := write(t, map[string]any{"run_tests": true})
		settings, err := LoadRigSettings(path)
		if err != nil {
			t.Fatalf("LoadRigSettings: %v", err)
		}
		if settings.MergeQueue.HasPathScope() {
			t.Errorf("HasPathScope() = true without allowed_paths")
		}
	})
}

func TestAllowedPathsFailClosedAndEmptyEdges(t *testing.T) {
	for _, bad := range []string{"/apps/**", "apps/[x", "apps/../x", ""} {
		if MatchAllowedPath(bad, "apps/x") {
			t.Errorf("MatchAllowedPath(%q) matched; malformed patterns must match nothing", bad)
		}
	}
	if got := PathsOutsideAllowed(nil, []string{"anything"}); got != nil {
		t.Errorf("PathsOutsideAllowed(nil) = %q, want nil (unrestricted)", got)
	}
	if got := PathsOutsideAllowed([]string{"apps/**"}, nil); got != nil {
		t.Errorf("PathsOutsideAllowed(no changes) = %q, want nil", got)
	}
}

func TestLoadMergeQueueAllowedPaths(t *testing.T) {
	dir := t.TempDir()
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name    string
		body    string
		want    []string
		wantErr bool
	}{
		// Regression (PR #38 review): an unrelated invalid field must not turn
		// an unscoped rig into a blocked one.
		{"invalid poll_interval without allowed_paths is unrestricted",
			`{"type":"rig-settings","version":1,"merge_queue":{"poll_interval":"bogus"}}`, nil, false},
		{"newer version and bad on_conflict without allowed_paths is unrestricted",
			`{"type":"rig-settings","version":99,"merge_queue":{"on_conflict":"nope","run_tests":"x"}}`, nil, false},
		{"allowed_paths still read when another field is invalid",
			`{"merge_queue":{"poll_interval":"bogus","allowed_paths":["apps/**"]}}`, []string{"apps/**"}, false},
		{"empty list is unrestricted", `{"merge_queue":{"allowed_paths":[]}}`, nil, false},
		{"no merge_queue is unrestricted", `{"type":"rig-settings"}`, nil, false},
		{"unparseable JSON fails closed", `{not json`, nil, true},
		{"allowed_paths of wrong type fails closed", `{"merge_queue":{"allowed_paths":"apps/**"}}`, nil, true},
		{"invalid pattern fails closed", `{"merge_queue":{"allowed_paths":["/apps/**"]}}`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadMergeQueueAllowedPaths(write(t, tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("missing file is unrestricted", func(t *testing.T) {
		got, err := LoadMergeQueueAllowedPaths(filepath.Join(t.TempDir(), "absent.json"))
		if err != nil || got != nil {
			t.Fatalf("got %q, %v; want nil, nil", got, err)
		}
	})
}
