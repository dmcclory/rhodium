package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"rhodium/internal/brain"
	"rhodium/internal/gh"
)

// TestSyncPRCache_PreservesOnAllReposFailing seeds the pr_cache with two
// PRs, then runs syncPRCache with an injected lister that errors for every
// configured repo. The cache MUST be left intact — wiping it and writing
// zero rows is the very bug being fixed.
func TestSyncPRCache_PreservesOnAllReposFailing(t *testing.T) {
	dir := t.TempDir()
	brainPath := filepath.Join(dir, "brain.db")
	t.Setenv("RHODIUM_BRAIN", brainPath)

	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"repos":["acme/web","acme/api"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RHODIUM_CONFIG", cfgPath)

	b, err := brain.LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// Seed the cache with two PRs.
	seed := []gh.PR{
		{Repo: "acme/web", Number: 1, Title: "first"},
		{Repo: "acme/api", Number: 2, Title: "second"},
	}
	if err := b.SetPRCache(seed); err != nil {
		t.Fatal(err)
	}

	// Inject a lister that errors for every repo (offline / auth lapse).
	origList := gh.ListPRsFn
	t.Cleanup(func() { gh.ListPRsFn = origList })
	gh.ListPRsFn = func(repo string) ([]gh.PR, error) {
		return nil, errors.New("gh: network unreachable")
	}

	if err := syncPRCache(b); err != nil {
		t.Fatalf("syncPRCache returned error: %v", err)
	}

	cached := b.CachedPRs()
	if len(cached) != 2 {
		t.Fatalf("pr_cache was wiped: got %d entries; want 2", len(cached))
	}
}

// TestSyncPRCache_WipesWhenAllReposEmpty confirms the legitimate "every
// repo returned 0 PRs" case still wipes the cache (existing behavior must
// not regress).
func TestSyncPRCache_WipesWhenAllReposEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RHODIUM_BRAIN", filepath.Join(dir, "brain.db"))

	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"repos":["acme/web"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RHODIUM_CONFIG", cfgPath)

	b, err := brain.LoadBrain()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.SetPRCache([]gh.PR{{Repo: "acme/web", Number: 1, Title: "first"}}); err != nil {
		t.Fatal(err)
	}

	origList := gh.ListPRsFn
	t.Cleanup(func() { gh.ListPRsFn = origList })
	gh.ListPRsFn = func(repo string) ([]gh.PR, error) {
		return nil, nil // success, but no PRs
	}

	if err := syncPRCache(b); err != nil {
		t.Fatalf("syncPRCache returned error: %v", err)
	}

	cached := b.CachedPRs()
	if len(cached) != 0 {
		t.Fatalf("pr_cache not wiped when all repos returned 0 PRs: got %d entries", len(cached))
	}
}

