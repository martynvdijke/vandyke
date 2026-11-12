package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// createDated inserts an entry and moves it to a fixed UTC date.
func createDated(t *testing.T, st *Store, word, meaning, person, day string, photo *Photo) Entry {
	t.Helper()
	entry, err := st.CreateEntry(context.Background(), word, meaning, person, photo)
	if err != nil {
		t.Fatalf("CreateEntry %q: %v", word, err)
	}
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	_, updated, err := st.UpdateEntry(context.Background(), EntryUpdate{
		ID: entry.ID, Word: word, Meaning: meaning, Person: person,
		CreatedAt: parsed.UTC(),
	})
	if err != nil {
		t.Fatalf("UpdateEntry %q: %v", word, err)
	}
	return updated
}

func TestYearReviewAggregates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}

	first := createDated(t, st, "kast", "kas", "Anna", "2024-01-05", nil)
	createDated(t, st, "feature", "euten", "Bob", "2024-03-02", nil)
	createDated(t, st, "boeke", "boeken", "Anna", "2024-03-19", nil)
	longest := createDated(t, st, "langsteverschrijving", "longest", "Bob", "2024-07-04", nil)
	createDated(t, st, "kort", "short", "Bob", "2024-07-20", nil)
	last := createDated(t, st, "", "", "", "2024-12-24", photo)

	review, err := st.YearReview(ctx, 2024)
	if err != nil {
		t.Fatalf("YearReview: %v", err)
	}
	if review.Year != 2024 || review.Total != 6 {
		t.Fatalf("year/total = %d/%d, want 2024/6", review.Year, review.Total)
	}
	if review.FirstEntry == nil || review.FirstEntry.ID != first.ID {
		t.Fatalf("first entry = %+v, want %d", review.FirstEntry, first.ID)
	}
	if review.LastEntry == nil || review.LastEntry.ID != last.ID {
		t.Fatalf("last entry = %+v, want %d", review.LastEntry, last.ID)
	}
	if review.BusiestMonth != 3 || review.BusiestMonthCount != 2 {
		t.Fatalf("busiest month = %d (%d), want March (2), earliest of a tie",
			review.BusiestMonth, review.BusiestMonthCount)
	}
	if len(review.Months) != 12 {
		t.Fatalf("months = %d, want 12", len(review.Months))
	}
	if review.Months[2].Count != 2 || review.Months[6].Count != 2 || review.Months[0].Count != 1 {
		t.Fatalf("month buckets = %+v", review.Months)
	}
	if len(review.TopPersons) != 2 || review.TopPersons[0].Person != "Bob" || review.TopPersons[0].Count != 3 {
		t.Fatalf("top persons = %+v", review.TopPersons)
	}
	if review.Longest == nil || review.Longest.ID != longest.ID {
		t.Fatalf("longest = %+v, want %d", review.Longest, longest.ID)
	}
	if review.WithPhoto != 1 || review.PhotoPct != 17 {
		t.Fatalf("photo = %d (%d%%), want 1 (17%%)", review.WithPhoto, review.PhotoPct)
	}
}

func TestYearReviewPersonTieBreaksByName(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	createDated(t, st, "een", "one", "Zoe", "2024-02-01", nil)
	createDated(t, st, "twee", "two", "Anna", "2024-02-02", nil)

	review, err := st.YearReview(ctx, 2024)
	if err != nil {
		t.Fatalf("YearReview: %v", err)
	}
	if len(review.TopPersons) != 2 {
		t.Fatalf("top persons = %+v", review.TopPersons)
	}
	if review.TopPersons[0].Person != "Anna" || review.TopPersons[1].Person != "Zoe" {
		t.Fatalf("tie order = %+v, want Anna then Zoe", review.TopPersons)
	}
}

func TestYearReviewLongestMisspellingTie(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	earlier := createDated(t, st, "abcd", "first", "", "2024-01-10", nil)
	createDated(t, st, "wxyz", "second", "", "2024-02-10", nil)

	review, err := st.YearReview(ctx, 2024)
	if err != nil {
		t.Fatalf("YearReview: %v", err)
	}
	if review.Longest == nil || review.Longest.ID != earlier.ID {
		t.Fatalf("longest = %+v, want the earlier entry %d", review.Longest, earlier.ID)
	}
}

func TestYearReviewEmptyAndTrashedYears(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	if _, err := st.YearReview(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty year error = %v, want ErrNotFound", err)
	}
	if _, err := st.YearReview(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed year error = %v, want ErrNotFound", err)
	}

	entry := createDated(t, st, "verdwenen", "gone", "", "2019-06-01", nil)
	review, err := st.YearReview(ctx, 2019)
	if err != nil || review.Total != 1 {
		t.Fatalf("live year = %+v (%v)", review, err)
	}
	if _, err := st.SoftDeleteEntries(ctx, []int64{entry.ID}); err != nil {
		t.Fatalf("SoftDeleteEntries: %v", err)
	}
	if _, err := st.YearReview(ctx, 2019); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trashed year error = %v, want ErrNotFound", err)
	}
}

func TestYearReviewPhotoOnlyYear(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)
	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}

	createDated(t, st, "", "", "", "2024-08-01", photo)
	review, err := st.YearReview(ctx, 2024)
	if err != nil {
		t.Fatalf("YearReview: %v", err)
	}
	if review.Total != 1 || review.WithPhoto != 1 || review.PhotoPct != 100 {
		t.Fatalf("photo-only review = %+v", review)
	}
	if review.Longest != nil {
		t.Fatalf("photo-only year named a longest misspelling: %+v", review.Longest)
	}
	if review.FirstEntry == nil || review.LastEntry == nil {
		t.Fatal("photo-only year lost its first or last entry")
	}
}
