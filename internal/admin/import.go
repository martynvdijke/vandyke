package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/media"
	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	// maxImportBytes caps both the uploaded file and the decompressed
	// entries.json payload.
	maxImportBytes int64 = 64 << 20
	// maxImportEntries caps the number of rows in one import.
	maxImportEntries = 20000
)

// importStatusError carries the HTTP status and operator message for a refused
// import.
type importStatusError struct {
	status  int
	message string
}

func (e importStatusError) Error() string { return e.message }

func importRefused(status int, format string, args ...any) error {
	return importStatusError{status: status, message: fmt.Sprintf(format, args...)}
}

// importFile is the JSON envelope accepted by an import.
type importFile struct {
	FormatVersion int               `json:"format_version"`
	Entries       []store.ExportRow `json:"entries"`
}

type importPageData struct {
	Admin         store.Admin
	Umami         config.Umami
	CSRF          string
	MaxBytes      int64
	MaxEntries    int
	MaxPhotoBytes int64
}

type importResultData struct {
	Admin     store.Admin
	Umami     config.Umami
	CSRF      string
	Preview   bool
	Mode      string
	Conflict  string
	Created   int
	Skipped   int
	Conflicts int
	Replaced  int
	Failed    int
	Error     string
}

func (a *Admin) handleImportForm(rw http.ResponseWriter, r *http.Request) {
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-import", importPageData{
		Admin:         state.Admin,
		Umami:         a.settings.Current(),
		CSRF:          state.Session.CSRFToken,
		MaxBytes:      maxImportBytes,
		MaxEntries:    maxImportEntries,
		MaxPhotoBytes: store.MaxPhotoBytes,
	})
}

func (a *Admin) handleImport(rw http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode == "" {
		mode = "preview"
	}
	if mode != "preview" && mode != "apply" {
		http.Error(rw, "mode must be preview or apply", http.StatusBadRequest)
		return
	}
	conflict := strings.TrimSpace(r.FormValue("conflict"))
	if conflict == "" {
		conflict = "skip"
	}
	if conflict != "skip" && conflict != "replace" {
		http.Error(rw, "conflict must be skip or replace", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(rw, "choose an export file to import", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxImportBytes+1))
	if err != nil {
		http.Error(rw, "could not read the uploaded file", http.StatusBadRequest)
		return
	}
	if int64(len(data)) > maxImportBytes {
		http.Error(rw, "import exceeds the 64 MiB request cap", http.StatusRequestEntityTooLarge)
		return
	}

	rows, err := parseImport(data)
	if err != nil {
		var refused importStatusError
		if errors.As(err, &refused) {
			http.Error(rw, refused.message, refused.status)
			return
		}
		a.internalError(rw, err)
		return
	}

	state := stateFrom(r)
	if mode == "preview" {
		preview, err := a.store.PreviewImport(r.Context(), rows)
		if err != nil {
			a.internalError(rw, err)
			return
		}
		a.render(rw, http.StatusOK, "admin-import-result", importResultData{
			Admin:     state.Admin,
			Umami:     a.settings.Current(),
			CSRF:      state.Session.CSRFToken,
			Preview:   true,
			Mode:      mode,
			Conflict:  conflict,
			Created:   preview.Create,
			Skipped:   preview.Skip,
			Conflicts: preview.Conflict,
		})
		return
	}

	result, err := a.store.ApplyImport(r.Context(), rows, conflict == "replace")
	if err != nil {
		var rowErr *store.ImportRowError
		if errors.As(err, &rowErr) {
			a.render(rw, http.StatusUnprocessableEntity, "admin-import-result", importResultData{
				Admin:    state.Admin,
				Umami:    a.settings.Current(),
				CSRF:     state.Session.CSRFToken,
				Mode:     mode,
				Conflict: conflict,
				Failed:   1,
				Error:    fmt.Sprintf("Row %d (%q) failed; nothing was imported. %s", rowErr.Index+1, rowErr.Word, rowErr.Err.Error()),
			})
			return
		}
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusOK, "admin-import-result", importResultData{
		Admin:    state.Admin,
		Umami:    a.settings.Current(),
		CSRF:     state.Session.CSRFToken,
		Mode:     mode,
		Conflict: conflict,
		Created:  result.Created,
		Skipped:  result.Skipped,
		Replaced: result.Replaced,
	})
}

// parseImport accepts either a JSON export envelope or a ZIP archive holding
// entries.json, an optional manifest.json and optional photos/{id}.jpg files.
func parseImport(data []byte) ([]store.ImportRow, error) {
	if bytes.HasPrefix(data, []byte("PK")) {
		return parseImportZip(data)
	}
	return parseImportJSON(data)
}

func parseImportJSON(data []byte) ([]store.ImportRow, error) {
	var file importFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, importRefused(http.StatusBadRequest, "could not parse the JSON export: %v", err)
	}
	if file.FormatVersion != exportFormatVersion {
		return nil, importRefused(http.StatusBadRequest,
			"unsupported format_version %d; this server supports %d", file.FormatVersion, exportFormatVersion)
	}
	if len(file.Entries) > maxImportEntries {
		return nil, importRefused(http.StatusRequestEntityTooLarge,
			"import exceeds the 20,000 entry cap")
	}
	rows := make([]store.ImportRow, 0, len(file.Entries))
	for _, entry := range file.Entries {
		rows = append(rows, store.ImportRow{
			ID:        entry.ID,
			Word:      entry.Word,
			Meaning:   entry.Meaning,
			Person:    entry.Person,
			CreatedAt: entry.CreatedAt,
		})
	}
	return rows, nil
}

func parseImportZip(data []byte) ([]store.ImportRow, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, importRefused(http.StatusBadRequest, "could not read the ZIP archive")
	}

	var (
		entriesData []byte
		manifest    *exportManifest
		photos      = make(map[int64][]byte)
		photoBytes  int64
	)
	for _, file := range reader.File {
		switch {
		case file.Name == "manifest.json":
			body, err := readZipFile(file, maxImportBytes)
			if err != nil {
				return nil, err
			}
			var decoded exportManifest
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, importRefused(http.StatusBadRequest, "could not parse manifest.json: %v", err)
			}
			manifest = &decoded
		case file.Name == "entries.json":
			body, err := readZipFile(file, maxImportBytes)
			if err != nil {
				return nil, err
			}
			entriesData = body
		case strings.HasPrefix(file.Name, "photos/") && strings.HasSuffix(file.Name, ".jpg"):
			id, ok := parsePhotoName(file.Name)
			if !ok {
				continue
			}
			body, err := readZipFile(file, store.MaxPhotoBytes)
			if err != nil {
				return nil, err
			}
			photoBytes += int64(len(body))
			if photoBytes > maxImportBytes {
				return nil, importRefused(http.StatusRequestEntityTooLarge,
					"import exceeds the 64 MiB request cap")
			}
			photos[id] = body
		}
	}

	if manifest != nil && manifest.FormatVersion != exportFormatVersion {
		return nil, importRefused(http.StatusBadRequest,
			"unsupported format_version %d; this server supports %d", manifest.FormatVersion, exportFormatVersion)
	}
	if entriesData == nil {
		return nil, importRefused(http.StatusBadRequest, "archive is missing entries.json")
	}

	rows, err := parseImportJSON(entriesData)
	if err != nil {
		return nil, err
	}

	entryIDs := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		entryIDs[row.ID] = struct{}{}
	}
	for id := range photos {
		if _, ok := entryIDs[id]; !ok {
			return nil, importRefused(http.StatusBadRequest,
				"archive has a photo for entry %d with no matching entry", id)
		}
	}

	for i := range rows {
		body, ok := photos[rows[i].ID]
		if !ok {
			continue
		}
		photo, err := importPhoto(body)
		if err != nil {
			return nil, err
		}
		rows[i].Photo = photo
	}
	return rows, nil
}

// readZipFile reads one archive member, refusing anything above limit bytes.
func readZipFile(file *zip.File, limit int64) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, importRefused(http.StatusBadRequest, "could not read %s", file.Name)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, importRefused(http.StatusBadRequest, "could not read %s", file.Name)
	}
	if int64(len(body)) > limit {
		if limit == store.MaxPhotoBytes {
			return nil, importRefused(http.StatusRequestEntityTooLarge,
				"a photo exceeds the 10 MiB per-photo cap")
		}
		return nil, importRefused(http.StatusRequestEntityTooLarge,
			"import exceeds the 64 MiB request cap")
	}
	return body, nil
}

func parsePhotoName(name string) (int64, bool) {
	raw := strings.TrimSuffix(strings.TrimPrefix(name, "photos/"), ".jpg")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// importPhoto validates and processes photo bytes with the existing upload and
// derivative limits.
func importPhoto(data []byte) (*store.Photo, error) {
	if int64(len(data)) > store.MaxPhotoBytes {
		return nil, importRefused(http.StatusRequestEntityTooLarge,
			"a photo exceeds the 10 MiB per-photo cap")
	}
	photo, err := store.NewPhoto(data)
	if err != nil {
		return nil, importRefused(http.StatusBadRequest, "a photo is not a supported image")
	}
	if err := media.Derive(photo); err != nil {
		return nil, importRefused(http.StatusBadRequest, "a photo could not be processed: %v", err)
	}
	return photo, nil
}
