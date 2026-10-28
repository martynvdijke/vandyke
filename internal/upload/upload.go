// Package upload reads uploaded files from HTTP requests.
package upload

import (
	"errors"
	"io"
	"net/http"

	"vandyke.thor.edu/vandyke/internal/media"
	"vandyke.thor.edu/vandyke/internal/store"
)

// Photo reads an optional photo field, returning nil when no file was sent.
func Photo(r *http.Request, field string) (*store.Photo, error) {
	file, _, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("could not read the photo")
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, store.MaxPhotoBytes+1))
	if err != nil {
		return nil, errors.New("could not read the photo")
	}
	if len(data) == 0 {
		return nil, nil
	}
	photo, err := store.NewPhoto(data)
	if err != nil {
		return nil, err
	}
	if err := media.Derive(photo); err != nil {
		return nil, err
	}
	return photo, nil
}
