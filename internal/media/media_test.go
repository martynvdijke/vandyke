package media_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"vandyke.thor.edu/vandyke/internal/media"
	"vandyke.thor.edu/vandyke/internal/store"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

const tinyWebPBase64 = "UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoKAAYAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA="

func tinyWebP(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(tinyWebPBase64)
	if err != nil {
		t.Fatalf("decode webp: %v", err)
	}
	return data
}

func TestDeriveVariants(t *testing.T) {
	source := testPNG(t, 2400, 1200)
	photo, err := store.NewPhoto(source)
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if err := media.Derive(photo); err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if photo.Width != 2400 || photo.Height != 1200 {
		t.Fatalf("original dimensions = %dx%d, want 2400x1200", photo.Width, photo.Height)
	}
	if photo.Display == nil || photo.Thumb == nil {
		t.Fatal("Derive did not create both variants")
	}
	if !bytes.Equal(photo.Data, source) {
		t.Fatal("Derive modified the original bytes")
	}

	display, err := jpeg.DecodeConfig(bytes.NewReader(photo.Display.Data))
	if err != nil {
		t.Fatalf("decode display: %v", err)
	}
	if display.Width != 1600 || display.Height != 800 {
		t.Fatalf("display dimensions = %dx%d, want 1600x800", display.Width, display.Height)
	}
	thumb, err := jpeg.DecodeConfig(bytes.NewReader(photo.Thumb.Data))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if thumb.Width != 320 || thumb.Height != 160 {
		t.Fatalf("thumb dimensions = %dx%d, want 320x160", thumb.Width, thumb.Height)
	}
	if photo.Display.ContentType != "image/jpeg" || photo.Thumb.ContentType != "image/jpeg" {
		t.Fatalf("variant content types = %q/%q, want image/jpeg", photo.Display.ContentType, photo.Thumb.ContentType)
	}
}

func TestDeriveJPEGAndWebP(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"jpeg", testJPEG(t, 640, 480)},
		{"webp", tinyWebP(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			photo, err := store.NewPhoto(tc.data)
			if err != nil {
				t.Fatalf("NewPhoto: %v", err)
			}
			if err := media.Derive(photo); err != nil {
				t.Fatalf("Derive: %v", err)
			}
			if photo.Display == nil || photo.Thumb == nil {
				t.Fatal("missing variants")
			}
		})
	}
}

func TestDeriveRejectsOversizedImage(t *testing.T) {
	photo, err := store.NewPhoto(testPNG(t, media.MaxSide+1, 1))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if err := media.Derive(photo); !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("Derive error = %v, want ErrTooLarge", err)
	}
}

func TestDeriveRejectsCorruptImage(t *testing.T) {
	photo, err := store.NewPhoto(append([]byte("\x89PNG\r\n\x1a\n"), []byte("not an image")...))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	if err := media.Derive(photo); !errors.Is(err, media.ErrDecode) {
		t.Fatalf("Derive error = %v, want ErrDecode", err)
	}
}

func TestBackfill(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	photo, err := store.NewPhoto(testPNG(t, 100, 50))
	if err != nil {
		t.Fatalf("NewPhoto: %v", err)
	}
	entry, err := st.CreateEntry(t.Context(), "foto", "photo", photo)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	missing, err := st.ListPhotosMissingDerivatives(t.Context())
	if err != nil {
		t.Fatalf("ListPhotosMissingDerivatives: %v", err)
	}
	if len(missing) != 1 || missing[0].EntryID != entry.ID {
		t.Fatalf("missing = %+v, want entry %d", missing, entry.ID)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := media.Backfill(t.Context(), st, logger); err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	display, err := st.GetEntryPhotoSize(t.Context(), entry.ID, "display")
	if err != nil {
		t.Fatalf("GetEntryPhotoSize display: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(display.Data))
	if err != nil || cfg.Width != 100 || cfg.Height != 50 {
		t.Fatalf("backfilled display = %dx%d, err %v; want 100x50 JPEG", cfg.Width, cfg.Height, err)
	}
	thumb, err := st.GetEntryPhotoSize(t.Context(), entry.ID, "thumb")
	if err != nil {
		t.Fatalf("GetEntryPhotoSize thumb: %v", err)
	}
	cfg, err = jpeg.DecodeConfig(bytes.NewReader(thumb.Data))
	if err != nil || cfg.Width != 100 || cfg.Height != 50 {
		t.Fatalf("backfilled thumb = %dx%d, err %v; want 100x50 JPEG", cfg.Width, cfg.Height, err)
	}
}
