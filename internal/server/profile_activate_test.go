package server

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/config"
)

// TestRefsMinus covers the diff helper that drives ActivateProfile.
// Identity is (kind, name) only; Args differences don't separate refs.
func TestRefsMinus(t *testing.T) {
	a := []config.SourceRef{
		{Kind: "docker", Name: "api"},
		{Kind: "file", Name: "/var/log/app.log"},
		{Kind: "docker", Name: "worker"},
	}
	b := []config.SourceRef{
		{Kind: "docker", Name: "worker"},
		{Kind: "docker", Name: "ingest"},
	}

	got := refsMinus(a, b)
	want := []config.SourceRef{
		{Kind: "docker", Name: "api"},
		{Kind: "file", Name: "/var/log/app.log"},
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d (got=%+v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i].Kind != want[i].Kind || got[i].Name != want[i].Name {
			t.Errorf("[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}

	// Empty inputs.
	if got := refsMinus(nil, b); got != nil {
		t.Errorf("nil-a: want nil, got %+v", got)
	}
	if got := refsMinus(a, nil); len(got) != len(a) {
		t.Errorf("nil-b: want all of a, got %+v", got)
	}

	// Args ignored — same kind+name → not in diff.
	a2 := []config.SourceRef{{Kind: "file", Name: "/x", Args: map[string]any{"path": "/x"}}}
	b2 := []config.SourceRef{{Kind: "file", Name: "/x"}}
	if got := refsMinus(a2, b2); len(got) != 0 {
		t.Errorf("args-ignored: got %+v want []", got)
	}
}

// TestActivateProfileFileOverlay drives the full ActivateProfile path with
// real file sources backing the refs:
//   - profile A has [a.log], profile B has [b.log], no overlap
//   - switch nil→A → a.log opens, owner=profile:A
//   - switch A→B → a.log closes, b.log opens
//   - switch B→A → b.log closes, a.log re-opens
//   - manual add /m.log survives both switches (different owner)
func TestActivateProfileFileOverlay(t *testing.T) {
	tmp := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	aPath := mk("a.log")
	bPath := mk("b.log")
	mPath := mk("m.log")

	srv := NewServer(Options{
		// Skip unix socket — macOS's 104-char path limit + long test
		// names blow up the bind. The activation logic doesn't need it.
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
		Profiles: []ProfileInfo{
			{Name: "A", Sources: []config.SourceRef{{Kind: "file", Name: aPath}}},
			{Name: "B", Sources: []config.SourceRef{{Kind: "file", Name: bPath}}},
		},
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// Manual add — survives switches.
	if _, err := srv.AddFileSource(mPath); err != nil {
		t.Fatal(err)
	}

	openNames := func() []string {
		out := []string{}
		for _, s := range srv.Sources() {
			if s.State == "open" {
				out = append(out, s.Name)
			}
		}
		sort.Strings(out)
		return out
	}
	wantOpen := func(label string, want ...string) {
		t.Helper()
		sort.Strings(want)
		got := openNames()
		if len(got) != len(want) {
			t.Fatalf("%s: open=%v want=%v", label, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: open=%v want=%v", label, got, want)
			}
		}
	}

	wantOpen("initial", mPath)

	if err := srv.ActivateProfile("A"); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	wantOpen("after A", mPath, aPath)

	if err := srv.ActivateProfile("B"); err != nil {
		t.Fatalf("activate B: %v", err)
	}
	wantOpen("after B", mPath, bPath)

	if err := srv.ActivateProfile("A"); err != nil {
		t.Fatalf("activate A again: %v", err)
	}
	wantOpen("after A again", mPath, aPath)

	// Activating "" tears down profile-owned, leaves manual.
	if err := srv.ActivateProfile(""); err != nil {
		t.Fatalf("activate '': %v", err)
	}
	wantOpen("after deactivate", mPath)

	// Unknown profile returns an error and doesn't change state.
	if err := srv.ActivateProfile("nope"); err == nil {
		t.Errorf("activate nope: want error, got nil")
	}
	wantOpen("after nope", mPath)
}

// TestActivateProfileSharedRef checks the dedup case: a manual add and a
// profile both reference the same file. Activating the profile must NOT
// tear down the manual on the next switch.
func TestActivateProfileSharedRef(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "shared.log")
	if err := os.WriteFile(p, []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Options{
		// Skip unix socket — macOS's 104-char path limit + long test
		// names blow up the bind. The activation logic doesn't need it.
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
		Profiles: []ProfileInfo{
			{Name: "A", Sources: []config.SourceRef{{Kind: "file", Name: p}}},
			{Name: "B"},
		},
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// Manual add first; profile A activation should hit the dedup and
	// NOT take ownership.
	if _, err := srv.AddFileSource(p); err != nil {
		t.Fatal(err)
	}
	if err := srv.ActivateProfile("A"); err != nil {
		t.Fatal(err)
	}
	// Switch to B — A's overlay should try to remove p but fail to find
	// it tagged "profile:A" (it's owner=""), so the source stays open.
	if err := srv.ActivateProfile("B"); err != nil {
		t.Fatal(err)
	}
	open := 0
	for _, s := range srv.Sources() {
		if s.State == "open" && s.Name == p {
			open++
		}
	}
	if open != 1 {
		t.Errorf("shared ref: expected 1 open instance after switch, got %d", open)
	}
}
