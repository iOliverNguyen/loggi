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
