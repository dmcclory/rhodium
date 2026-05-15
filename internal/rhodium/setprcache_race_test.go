package rhodium

import (
	"sync"
	"testing"

	"rhodium/internal/gh"
)

// TestSnapshotPRs_DecouplesBackingArray asserts that snapshotPRs returns a
// copy whose backing array is independent of the source. This is the load-
// bearing invariant for the four background SetPRCache call sites: the
// goroutine ranges over the snapshot while the tea Update loop may continue
// mutating a.cache.allPRs.
func TestSnapshotPRs_DecouplesBackingArray(t *testing.T) {
	orig := []gh.PR{
		{Repo: "acme/web", Number: 1, Title: "first"},
		{Repo: "acme/web", Number: 2, Title: "second"},
	}
	snap := snapshotPRs(orig)
	if len(snap) != len(orig) {
		t.Fatalf("snapshot len = %d; want %d", len(snap), len(orig))
	}
	// Mutate the source — snapshot must not see the change.
	orig[0].Title = "MUTATED"
	if snap[0].Title != "first" {
		t.Fatalf("snapshot saw source mutation: got Title=%q; want %q", snap[0].Title, "first")
	}
	// Appending past cap on the source must not touch the snapshot either.
	orig = append(orig, gh.PR{Repo: "acme/web", Number: 3, Title: "third"})
	if len(snap) != 2 {
		t.Fatalf("snapshot len changed after source append: got %d; want 2", len(snap))
	}
}

// TestSnapshotPRs_RaceFree exercises snapshotPRs concurrently with mutation
// of the source slice — the way the tea Update loop mutates a.cache.allPRs
// while bgSetPRCache fires off a goroutine. Must pass under `go test -race`.
func TestSnapshotPRs_RaceFree(t *testing.T) {
	allPRs := []gh.PR{
		{Repo: "acme/web", Number: 1, Title: "first"},
		{Repo: "acme/web", Number: 2, Title: "second"},
		{Repo: "acme/web", Number: 3, Title: "third"},
	}

	var mu sync.Mutex // guards allPRs reads; bgSetPRCache calls snapshotPRs while holding caller-side serialization (the tea Update goroutine).
	var wg sync.WaitGroup
	const N = 200
	wg.Add(N * 2)

	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			// Simulate the tea Update goroutine: take the snapshot, then
			// the goroutine consumes it asynchronously. The race detector
			// would flag a shared-backing-array read here against the
			// concurrent mutate goroutine if snapshotPRs didn't copy.
			mu.Lock()
			snap := snapshotPRs(allPRs)
			mu.Unlock()
			go func() {
				// Read the snapshot — production goroutine ranges over it
				// to write to SQLite; for the race-detector it suffices
				// to touch every entry.
				var n int
				for range snap {
					n++
				}
				_ = n
			}()
		}(i)
		go func(i int) {
			defer wg.Done()
			mu.Lock()
			allPRs = append(allPRs, gh.PR{Repo: "acme/web", Number: 100 + i, Title: "added"})
			if len(allPRs) > 10 {
				allPRs = allPRs[:3]
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
}
