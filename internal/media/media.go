// Package media turns uploaded photos into safe, fast display variants.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log/slog"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"

	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	// DisplayMaxSide is the longest side of the public display image.
	DisplayMaxSide = 1600
	// ThumbMaxSide is the longest side of the ledger thumbnail.
	ThumbMaxSide = 320
	// MaxPixels rejects images that are unreasonably large to decode.
	MaxPixels = 40_000_000
	// MaxSide rejects images with an extreme aspect ratio or dimension.
	MaxSide = 12_000
)

var (
	// ErrTooLarge is returned for images beyond the decode limits.
	ErrTooLarge = errors.New("photo dimensions are too large (maximum 40 megapixels)")
	// ErrDecode is returned when the image cannot be decoded.
	ErrDecode = errors.New("photo could not be decoded")
)

// Derive decodes photo.Data, applies orientation, strips metadata by
// re-encoding, and stores display and thumbnail variants on the photo.
func Derive(photo *store.Photo) error {
	if photo == nil || len(photo.Data) == 0 {
		return errors.New("photo is empty")
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(photo.Data))
	if err != nil {
		return ErrDecode
	}
	if cfg.Width > MaxSide || cfg.Height > MaxSide ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return ErrTooLarge
	}

	img, err := decode(photo)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	photo.Width = bounds.Dx()
	photo.Height = bounds.Dy()

	display, err := variant(img, DisplayMaxSide)
	if err != nil {
		return err
	}
	thumb, err := variant(img, ThumbMaxSide)
	if err != nil {
		return err
	}
	photo.Display = display
	photo.Thumb = thumb
	return nil
}

func decode(photo *store.Photo) (image.Image, error) {
	if photo.ContentType == "image/webp" {
		img, err := webp.Decode(bytes.NewReader(photo.Data))
		if err != nil {
			return nil, ErrDecode
		}
		return img, nil
	}
	img, err := imaging.Decode(bytes.NewReader(photo.Data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, ErrDecode
	}
	return img, nil
}

func variant(img image.Image, maxSide int) (*store.PhotoVariant, error) {
	resized := imaging.Fit(img, maxSide, maxSide, imaging.Lanczos)
	canvas := image.NewRGBA(image.Rect(0, 0, resized.Bounds().Dx(), resized.Bounds().Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), resized, resized.Bounds().Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return nil, fmt.Errorf("encode photo variant: %w", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return &store.PhotoVariant{
		ContentType: "image/jpeg",
		Data:        data,
		SHA256:      hex.EncodeToString(sum[:]),
		Size:        len(data),
		Width:       canvas.Bounds().Dx(),
		Height:      canvas.Bounds().Dy(),
	}, nil
}

// Backfill processes photos that were stored before the derivative pipeline.
// Individual failures are logged and skipped so one corrupt photo cannot stop
// the server from starting.
func Backfill(ctx context.Context, st *store.Store, logger *slog.Logger) error {
	photos, err := st.ListPhotosMissingDerivatives(ctx)
	if err != nil {
		return err
	}
	for _, item := range photos {
		photo := &store.Photo{
			ContentType: item.ContentType,
			Data:        item.Data,
			SHA256:      item.SHA256,
			Size:        item.Size,
		}
		if err := Derive(photo); err != nil {
			logger.Warn("backfill photo derivative", "entry_id", item.EntryID, "error", err)
			continue
		}
		if err := st.SetPhotoDerivatives(ctx, item.EntryID, photo.Width, photo.Height, *photo.Display, *photo.Thumb); err != nil {
			logger.Warn("save photo derivative", "entry_id", item.EntryID, "error", err)
		}
	}
	return nil
}
