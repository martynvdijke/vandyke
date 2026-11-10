package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExportEntriesMapping(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	withPhoto := insertAttributed(t, st, "alpha", "first", "Alice", "2020-01-02T03:04:05Z")
	attachRawPhoto(t, st, withPhoto)
	withoutPhoto := insertRaw(t, st, "beta", "second", "2021-02-03T04:05:06Z")

	rows, err := st.ExportEntries(ctx)
	if err != nil {
		t.Fatalf("ExportEntries: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	first := rows[0]
	if first.ID != withPhoto || first.Word != "alpha" || first.Meaning != "first" || first.Person != "Alice" {
		t.Errorf("first row = %+v", first)
	}
	if !first.HasPhoto || first.PhotoContentType != "image/png" || first.PhotoSHA256 != "raw-test-sha256" {
		t.Errorf("first photo metadata = %+v", first)
	}
	if got := first.CreatedAt.Format(time.RFC3339); got != "2020-01-02T03:04:05Z" {
		t.Errorf("first created_at = %s", got)
	}

	second := rows[1]
	if second.ID != withoutPhoto || second.HasPhoto || second.PhotoContentType != "" || second.PhotoSHA256 != "" {
		t.Errorf("second row = %+v", second)
	}
}

func TestGetEntryExportPhotoFallsBackToOriginal(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	photo, err := NewPhoto(tinyPNG(t))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(ctx, "foto", "photo", "", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	got, err := st.GetEntryExportPhoto(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetEntryExportPhoto: %v", err)
	}
	if got.ContentType != "image/png" || !bytes.Equal(got.Data, photo.Data) {
		t.Fatalf("fallback photo = %s, %d bytes; want the original PNG", got.ContentType, len(got.Data))
	}

	// Store a display derivative and confirm the derivative wins.
	display := []byte("display-bytes")
	if _, err := st.db.Exec(
		`UPDATE entry_photos SET display_content_type = ?, display_data = ? WHERE entry_id = ?`,
		"image/jpeg", display, entry.ID,
	); err != nil {
		t.Fatalf("store derivative: %v", err)
	}
	got, err = st.GetEntryExportPhoto(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetEntryExportPhoto derivative: %v", err)
	}
	if got.ContentType != "image/jpeg" || !bytes.Equal(got.Data, display) {
		t.Fatalf("derivative photo = %s %q, want image/jpeg %q", got.ContentType, got.Data, display)
	}

	if _, err := st.GetEntryExportPhoto(ctx, entry.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing export photo error = %v, want ErrNotFound", err)
	}
}

func TestPreviewImportClassification(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	clearEntries(t, st)

	if _, err := st.CreateEntry(ctx, "Existing", "row", "", nil); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	preview, err := st.PreviewImport(ctx, []ImportRow{
		{Word: "new", Meaning: "row"},
		{Word: "existing", Meaning: "ROW"},
		{Word: "new", Meaning: "row"},
	})
	if err != nil {
		t.Fatalf("PreviewImport: %v", err)
	}
	if preview.Create != 1 || preview.Conflict != 1 || preview.Skip != 1 {
		t.Fatalf("preview = %+v, want create=1 conflict=1 skip=1", preview)
	}

	// Preview writes nothing.
	count, err := st.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 1 {
		t.Fatalf("count after preview = %d, want 1", count)
	}
}

func TestApplyImportInsertSkipReplace(t *testing.T) {
	ctx := context.Background()

	t.Run("insert on empty", func(t *testing.T) {
		st := newTestStore(t)
		clearEntries(t, st)

		photo, err := NewPhoto(tinyPNG(t))
		if err != nil {
			t.Fatalf("NewPhoto: %v", err)
		}
		when := time.Date(2019, 4, 5, 12, 30, 0, 0, time.UTC)
		result, err := st.ApplyImport(ctx, []ImportRow{
			{Word: "alpha", Meaning: "first", Person: "Alice", CreatedAt: when},
			{Word: "beta", Meaning: "second", Photo: photo},
		}, false)
		if err != nil {
			t.Fatalf("ApplyImport: %v", err)
		}
		if result.Created != 2 || result.Skipped != 0 || result.Replaced != 0 {
			t.Fatalf("result = %+v", result)
		}
		count, err := st.CountEntries(ctx)
		if err != nil {
			t.Fatalf("CountEntries: %v", err)
		}
		if count != 2 {
			t.Fatalf("count = %d, want 2", count)
		}
		page, err := st.ListEntries(ctx, ListParams{Query: "alpha"})
		if err != nil {
			t.Fatalf("ListEntries: %v", err)
		}
		if page.Total != 1 || page.Entries[0].Person != "Alice" {
			t.Fatalf("search after import = %+v", page)
		}
		if got := page.Entries[0].CreatedAt.Format(time.RFC3339); got != when.Format(time.RFC3339) {
			t.Fatalf("inserted created_at = %s, want %s", got, when.Format(time.RFC3339))
		}
		photoPage, err := st.ListEntries(ctx, ListParams{Query: "beta"})
		if err != nil {
			t.Fatalf("ListEntries beta: %v", err)
		}
		if photoPage.Total != 1 || photoPage.Entries[0].PhotoURL == "" {
			t.Fatal("imported photo missing")
		}
	})

	t.Run("skip on duplicate", func(t *testing.T) {
		st := newTestStore(t)
		clearEntries(t, st)

		original, err := st.CreateEntry(ctx, "Existing", "row", "Alice", nil)
		if err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
		result, err := st.ApplyImport(ctx, []ImportRow{
			{Word: "existing", Meaning: "ROW", Person: "Bob", CreatedAt: time.Now()},
		}, false)
		if err != nil {
			t.Fatalf("ApplyImport: %v", err)
		}
		if result.Skipped != 1 || result.Created != 0 || result.Replaced != 0 {
			t.Fatalf("result = %+v", result)
		}
		stored, err := st.GetEntry(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetEntry: %v", err)
		}
		if stored.Word != "Existing" || stored.Person != "Alice" {
			t.Fatalf("duplicate changed stored row: %+v", stored)
		}
	})

	t.Run("replace on conflict", func(t *testing.T) {
		st := newTestStore(t)
		clearEntries(t, st)

		original, err := st.CreateEntry(ctx, "Existing", "row", "Alice", nil)
		if err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
		when := time.Date(2018, 3, 2, 1, 2, 3, 0, time.UTC)
		newPhoto := &Photo{
			ContentType: "image/jpeg",
			Data:        []byte("original-bytes"),
			SHA256:      "new-original-sha",
			Size:        14,
			Display: &PhotoVariant{
				ContentType: "image/jpeg",
				Data:        []byte("display-bytes"),
				SHA256:      "new-display-sha",
				Size:        13,
			},
		}
		result, err := st.ApplyImport(ctx, []ImportRow{
			{Word: "EXISTING", Meaning: "ROW", Person: "Bob", CreatedAt: when, Photo: newPhoto},
		}, true)
		if err != nil {
			t.Fatalf("ApplyImport: %v", err)
		}
		if result.Replaced != 1 || result.Created != 0 || result.Skipped != 0 {
			t.Fatalf("result = %+v", result)
		}
		stored, err := st.GetEntry(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetEntry: %v", err)
		}
		if stored.Word != "Existing" || stored.Meaning != "row" {
			t.Fatalf("replace changed identity: %+v", stored)
		}
		if stored.Person != "Bob" || !stored.CreatedAt.Equal(when) {
			t.Fatalf("replace did not update person/date: %+v", stored)
		}
		got, err := st.GetEntryExportPhoto(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetEntryExportPhoto: %v", err)
		}
		if got.ContentType != "image/jpeg" || string(got.Data) != "display-bytes" {
			t.Fatalf("replaced photo = %s %q", got.ContentType, got.Data)
		}
	})

	t.Run("rollback on mid-file error", func(t *testing.T) {
		st := newTestStore(t)
		clearEntries(t, st)

		_, err := st.ApplyImport(ctx, []ImportRow{
			{Word: "valid", Meaning: "first"},
			{Word: strings.Repeat("x", MaxWordLen+1), Meaning: "too long"},
		}, false)
		if err == nil {
			t.Fatal("ApplyImport succeeded, want a row error")
		}
		var rowErr *ImportRowError
		if !errors.As(err, &rowErr) {
			t.Fatalf("error = %v, want *ImportRowError", err)
		}
		if rowErr.Index != 1 || !strings.HasPrefix(rowErr.Word, "xxx") {
			t.Fatalf("row error = %+v", rowErr)
		}
		count, err := st.CountEntries(ctx)
		if err != nil {
			t.Fatalf("CountEntries: %v", err)
		}
		if count != 0 {
			t.Fatalf("count after rollback = %d, want 0", count)
		}
	})

	t.Run("duplicate within file is skipped", func(t *testing.T) {
		st := newTestStore(t)
		clearEntries(t, st)

		result, err := st.ApplyImport(ctx, []ImportRow{
			{Word: "same", Meaning: "row"},
			{Word: "same", Meaning: "row"},
		}, false)
		if err != nil {
			t.Fatalf("ApplyImport: %v", err)
		}
		if result.Created != 1 || result.Skipped != 1 {
			t.Fatalf("result = %+v", result)
		}
	})
}
