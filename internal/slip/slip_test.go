package slip

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

func newTestResolver(t *testing.T) (*Resolver, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "slip.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st
}

// clearLedger trashes and purges every entry so the absence paths can be
// exercised without touching the settings table directly.
func clearLedger(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	ids, err := st.AllEntryIDs(ctx)
	if err != nil {
		t.Fatalf("AllEntryIDs: %v", err)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := st.SoftDeleteEntries(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, ids); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
}

func TestForDayIsStableWithinADay(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	first, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if first.Date != "2026-11-11" {
		t.Fatalf("date = %q, want 2026-11-11", first.Date)
	}
	if first.Entry.ID <= 0 || first.Entry.DeletedAt != nil {
		t.Fatalf("entry = %+v, want a live entry", first.Entry)
	}

	second, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay second: %v", err)
	}
	if second.Entry.ID != first.Entry.ID {
		t.Fatalf("two resolutions differ: %d then %d", first.Entry.ID, second.Entry.ID)
	}

	// The pin is the settings row the resolver read both times.
	pinned, exists, err := st.GetSetting(ctx, PinKey("2026-11-11"))
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !exists || pinned != strconv.FormatInt(first.Entry.ID, 10) {
		t.Fatalf("pin = %q, %v; want the served id", pinned, exists)
	}
}

func TestTodayAgreesWithForDay(t *testing.T) {
	ctx := context.Background()
	resolver, _ := newTestResolver(t)

	today, err := resolver.Today(ctx)
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	explicit, err := resolver.ForDay(ctx, LocalDay(time.Now()))
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if explicit.Date != today.Date || explicit.Entry.ID != today.Entry.ID {
		t.Fatalf("Today and ForDay disagree: %q/%d vs %q/%d",
			today.Date, today.Entry.ID, explicit.Date, explicit.Entry.ID)
	}
	// A second Today call must serve the same entry.
	again, err := resolver.Today(ctx)
	if err != nil {
		t.Fatalf("Today second: %v", err)
	}
	if again.Entry.ID != today.Entry.ID {
		t.Fatalf("Today changed within the same day: %d then %d", today.Entry.ID, again.Entry.ID)
	}
}

func TestPinSurvivesAnEntryAddedMidDay(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	before, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if _, err := st.CreateEntry(ctx, "nieuw", "new slip", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	after, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay after add: %v", err)
	}
	if after.Entry.ID != before.Entry.ID {
		t.Fatalf("an added entry changed the day's pick: %d then %d", before.Entry.ID, after.Entry.ID)
	}
}

func TestDateChangePinsNewDayAndPrunesOld(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	if _, err := resolver.ForDay(ctx, "2026-11-10"); err != nil {
		t.Fatalf("ForDay day one: %v", err)
	}
	second, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay day two: %v", err)
	}

	settings, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if _, ok := settings[PinKey("2026-11-10")]; ok {
		t.Error("the previous day's pin was not pruned")
	}
	if settings[PinKey("2026-11-11")] != strconv.FormatInt(second.Entry.ID, 10) {
		t.Errorf("new pin = %q, want %d", settings[PinKey("2026-11-11")], second.Entry.ID)
	}
}

func TestRepinWhenPinnedEntryIsTrashed(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	pinned, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{pinned.Entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}

	repinned, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay after trash: %v", err)
	}
	if repinned.Entry.ID == pinned.Entry.ID {
		t.Fatal("the trashed entry was served again")
	}
	if repinned.Entry.ID <= 0 {
		t.Fatal("repin returned no entry")
	}
	stored, _, err := st.GetSetting(ctx, PinKey("2026-11-11"))
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if stored != strconv.FormatInt(repinned.Entry.ID, 10) {
		t.Fatalf("stored pin = %q, want the repinned id %d", stored, repinned.Entry.ID)
	}
}

func TestRepinWhenPinnedEntryIsPurged(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	pinned, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{pinned.Entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	// A purged pick must not come back; the resolver picks from the survivors.
	if _, err := resolver.ForDay(ctx, "2026-11-11"); err != nil {
		t.Fatalf("ForDay after trash: %v", err)
	}
	if _, err := st.PurgeEntries(ctx, []int64{pinned.Entry.ID}); err != nil {
		t.Fatalf("PurgeEntries: %v", err)
	}
	if _, err := resolver.ForDay(ctx, "2026-11-11"); err != nil {
		t.Fatalf("ForDay after purge: %v", err)
	}
}

func TestNoSlipOnEmptyLedger(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)
	clearLedger(t, st)

	if _, err := resolver.ForDay(ctx, "2026-11-11"); !errors.Is(err, ErrNoSlip) {
		t.Fatalf("ForDay on empty ledger = %v, want ErrNoSlip", err)
	}
	if _, err := resolver.Today(ctx); !errors.Is(err, ErrNoSlip) {
		t.Fatalf("Today on empty ledger = %v, want ErrNoSlip", err)
	}
}

func TestPurgedPickWithNoSurvivorsReportsNoSlip(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	pinned, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	clearLedger(t, st)

	if _, err := resolver.ForDay(ctx, "2026-11-11"); !errors.Is(err, ErrNoSlip) {
		t.Fatalf("ForDay with no survivors = %v, want ErrNoSlip", err)
	}
	if _, err := st.GetEntryIncludingDeleted(ctx, pinned.Entry.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the purged entry still resolves: %v", err)
	}
}

func TestCorruptPinIsReplaced(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	if err := st.SetSettings(ctx, map[string]string{PinKey("2026-11-11"): "not-a-number"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	resolved, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	stored, _, err := st.GetSetting(ctx, PinKey("2026-11-11"))
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if stored != strconv.FormatInt(resolved.Entry.ID, 10) {
		t.Fatalf("corrupt pin = %q, want %d", stored, resolved.Entry.ID)
	}
	if _, err := st.GetEntry(ctx, resolved.Entry.ID); err != nil {
		t.Fatalf("served entry does not resolve: %v", err)
	}
}

func TestNegativePinIsReplaced(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	if err := st.SetSettings(ctx, map[string]string{PinKey("2026-11-11"): "-4"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	resolved, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if resolved.Entry.ID <= 0 {
		t.Fatalf("served invalid id %d", resolved.Entry.ID)
	}
}

func TestConcurrentFirstResolutionsAgree(t *testing.T) {
	ctx := context.Background()
	resolver, st := newTestResolver(t)

	const workers = 8
	results := make([]int64, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resolved, err := resolver.ForDay(ctx, "2026-11-11")
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = resolved.Entry.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	want := results[0]
	for i, got := range results {
		if got != want {
			t.Fatalf("worker %d got %d, worker 0 got %d", i, got, want)
		}
	}

	settings, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	pins := 0
	for key := range settings {
		if key == PinKey("2026-11-11") {
			pins++
		}
	}
	if pins != 1 {
		t.Fatalf("daily pins = %d, want exactly 1", pins)
	}
}

func TestPinPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "slip-persist.db")

	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	resolver := New(st)
	first, err := resolver.ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("store.Open reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	second, err := New(reopened).ForDay(ctx, "2026-11-11")
	if err != nil {
		t.Fatalf("ForDay after reopen: %v", err)
	}
	if second.Entry.ID != first.Entry.ID {
		t.Fatalf("pin did not survive a restart: %d then %d", first.Entry.ID, second.Entry.ID)
	}
}

func TestCandidateIndexIsDeterministic(t *testing.T) {
	for _, n := range []int{1, 2, 7, 78, 1000} {
		first := candidateIndex("2026-11-11", n)
		second := candidateIndex("2026-11-11", n)
		if first != second {
			t.Fatalf("candidateIndex(%d) is not deterministic: %d vs %d", n, first, second)
		}
		if first < 0 || first >= n {
			t.Fatalf("candidateIndex(%d) = %d, out of range", n, first)
		}
	}
	if candidateIndex("2026-11-11", 0) != 0 {
		t.Fatal("candidateIndex with no entries should be 0")
	}
}

func TestForDayRejectsMalformedDate(t *testing.T) {
	resolver, _ := newTestResolver(t)
	if _, err := resolver.ForDay(context.Background(), "11-11-2026"); err == nil {
		t.Fatal("ForDay accepted a malformed date")
	}
}
