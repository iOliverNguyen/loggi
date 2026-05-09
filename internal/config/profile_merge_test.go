package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProfilesMergeByName: when user config and repo config both define
// profiles, the merged result should contain both — not just the repo's.
// Same-named profiles are overridden by the later (repo) definition.
func TestProfilesMergeByName(t *testing.T) {
	tmp := t.TempDir()
	user := filepath.Join(tmp, "user.toml")
	repo := filepath.Join(tmp, "repo.toml")
	if err := os.WriteFile(user, []byte(`
[[profiles]]
name = "user-only"
filter = "level:info"

[[profiles]]
name = "shared"
filter = "level:debug"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo, []byte(`
[[profiles]]
name = "shared"
filter = "level:warn"

[[profiles]]
name = "repo-only"
filter = "level:error"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Defaults()
	var found []string
	if err := mergeFile(&cfg, user, &found); err != nil {
		t.Fatal(err)
	}
	if err := mergeFile(&cfg, repo, &found); err != nil {
		t.Fatal(err)
	}

	byName := make(map[string]string)
	for _, p := range cfg.Profiles {
		byName[p.Name] = p.Filter
	}

	// "default" comes from Defaults().
	if _, ok := byName["user-only"]; !ok {
		t.Errorf("missing user-only profile")
	}
	if _, ok := byName["repo-only"]; !ok {
		t.Errorf("missing repo-only profile")
	}
	if got := byName["shared"]; got != "level:warn" {
		t.Errorf("shared filter: want repo override 'level:warn', got %q", got)
	}

	// Order assertion: a-first preserved, new b-entries appended.
	// Defaults adds "default"; user adds [user-only, shared]; repo overrides
	// shared in place and appends repo-only.
	wantOrder := []string{"default", "user-only", "shared", "repo-only"}
	gotOrder := make([]string, len(cfg.Profiles))
	for i, p := range cfg.Profiles {
		gotOrder[i] = p.Name
	}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("profile order length: want %v got %v", wantOrder, gotOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("profile order: want %v got %v", wantOrder, gotOrder)
		}
	}
}

// TestAutostartAndProfileSourcesMerge: Sources.Autostart and Profile.Sources
// must round-trip across multi-file merges. Autostart in user config must
// survive being absent from repo config (merge isn't supposed to clobber
// scalar slices that aren't redefined in a later layer).
func TestAutostartAndProfileSourcesMerge(t *testing.T) {
	tmp := t.TempDir()
	user := filepath.Join(tmp, "user.toml")
	repo := filepath.Join(tmp, "repo.toml")
	if err := os.WriteFile(user, []byte(`
[[sources.autostart]]
kind = "docker"
name = "api"

[[profiles]]
name = "alpha"
[[profiles.sources]]
kind = "file"
name = "/var/log/a.log"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// repo overrides "alpha" with a different profile.sources list
	if err := os.WriteFile(repo, []byte(`
[[profiles]]
name = "alpha"
[[profiles.sources]]
kind = "docker"
name = "worker"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Defaults()
	var found []string
	if err := mergeFile(&cfg, user, &found); err != nil {
		t.Fatal(err)
	}
	if err := mergeFile(&cfg, repo, &found); err != nil {
		t.Fatal(err)
	}

	// Autostart was only set in user; repo doesn't redeclare it. Must survive.
	if got := len(cfg.Sources.Autostart); got != 1 {
		t.Fatalf("autostart: want 1 entry, got %d (%+v)", got, cfg.Sources.Autostart)
	}
	if cfg.Sources.Autostart[0].Kind != "docker" || cfg.Sources.Autostart[0].Name != "api" {
		t.Errorf("autostart entry: got %+v", cfg.Sources.Autostart[0])
	}

	// alpha.sources should be the repo override (last wins on profile by name).
	var alpha *Profile
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == "alpha" {
			alpha = &cfg.Profiles[i]
			break
		}
	}
	if alpha == nil {
		t.Fatalf("missing alpha profile")
	}
	if len(alpha.Sources) != 1 || alpha.Sources[0].Kind != "docker" || alpha.Sources[0].Name != "worker" {
		t.Errorf("alpha.sources after override: got %+v", alpha.Sources)
	}
}
