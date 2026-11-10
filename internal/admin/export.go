package admin

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/store"
)

// exportFormatVersion is the only export/import format this server writes and
// accepts.
const exportFormatVersion = 1

// exportEnvelope is the JSON export shape and the entries.json payload inside
// an export archive.
type exportEnvelope struct {
	FormatVersion int               `json:"format_version"`
	ExportedAt    string            `json:"exported_at"`
	EntryCount    int               `json:"entry_count"`
	PhotoCount    int               `json:"photo_count"`
	Entries       []store.ExportRow `json:"entries"`
}

// exportManifest is the format contract inside an export archive.
type exportManifest struct {
	FormatVersion int    `json:"format_version"`
	ExportedAt    string `json:"exported_at"`
	EntryCount    int    `json:"entry_count"`
	PhotoCount    int    `json:"photo_count"`
}

func exportFilename(extension string, at time.Time) string {
	return fmt.Sprintf("vandyke-ledger-%s%s", at.UTC().Format("2006-01-02"), extension)
}

func setExportHeaders(rw http.ResponseWriter, contentType, extension string, at time.Time) {
	rw.Header().Set("Content-Type", contentType)
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", exportFilename(extension, at)))
	rw.Header().Set("Cache-Control", "no-store")
}

func countPhotos(rows []store.ExportRow) int {
	n := 0
	for _, row := range rows {
		if row.HasPhoto {
			n++
		}
	}
	return n
}

func buildExportEnvelope(rows []store.ExportRow, at time.Time) exportEnvelope {
	return exportEnvelope{
		FormatVersion: exportFormatVersion,
		ExportedAt:    at.UTC().Format(time.RFC3339),
		EntryCount:    len(rows),
		PhotoCount:    countPhotos(rows),
		Entries:       rows,
	}
}

func (a *Admin) handleExportJSON(rw http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ExportEntries(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	at := time.Now().UTC()
	setExportHeaders(rw, "application/json; charset=utf-8", ".json", at)
	httpx.WriteJSON(rw, http.StatusOK, buildExportEnvelope(rows, at))
}

func (a *Admin) handleExportCSV(rw http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ExportEntries(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	at := time.Now().UTC()
	setExportHeaders(rw, "text/csv; charset=utf-8", ".csv", at)
	rw.WriteHeader(http.StatusOK)

	w := csv.NewWriter(rw)
	_ = w.Write([]string{
		"id", "word", "meaning", "person", "created_at",
		"has_photo", "photo_content_type", "photo_sha256",
	})
	for _, row := range rows {
		_ = w.Write([]string{
			strconv.FormatInt(row.ID, 10),
			row.Word,
			row.Meaning,
			row.Person,
			row.CreatedAt.UTC().Format(time.RFC3339),
			strconv.FormatBool(row.HasPhoto),
			row.PhotoContentType,
			row.PhotoSHA256,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		a.logger.Error("write export csv", "error", err)
	}
}

func writeZipJSON(zw *zip.Writer, name string, value any) error {
	entry, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	enc := json.NewEncoder(entry)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func (a *Admin) handleExportZIP(rw http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ExportEntries(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	at := time.Now().UTC()
	setExportHeaders(rw, "application/zip", ".zip", at)

	zw := zip.NewWriter(rw)
	envelope := buildExportEnvelope(rows, at)
	manifest := exportManifest{
		FormatVersion: envelope.FormatVersion,
		ExportedAt:    envelope.ExportedAt,
		EntryCount:    envelope.EntryCount,
		PhotoCount:    envelope.PhotoCount,
	}
	if err := writeZipJSON(zw, "manifest.json", manifest); err != nil {
		a.logger.Error("write export manifest", "error", err)
		return
	}
	if err := writeZipJSON(zw, "entries.json", envelope); err != nil {
		a.logger.Error("write export entries", "error", err)
		return
	}
	for _, row := range rows {
		if !row.HasPhoto {
			continue
		}
		photo, err := a.store.GetEntryExportPhoto(r.Context(), row.ID)
		if err != nil {
			a.logger.Error("read export photo", "entry_id", row.ID, "error", err)
			return
		}
		entry, err := zw.Create(fmt.Sprintf("photos/%d.jpg", row.ID))
		if err != nil {
			a.logger.Error("create export photo", "entry_id", row.ID, "error", err)
			return
		}
		if _, err := entry.Write(photo.Data); err != nil {
			a.logger.Error("write export photo", "entry_id", row.ID, "error", err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		a.logger.Error("close export archive", "error", err)
	}
}
