package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

// tagOption is one checkbox in the entry edit form's tag fieldset.
type tagOption struct {
	ID       int64
	Name     string
	Slug     string
	Color    string
	Selected bool
}

// tagRow is one vocabulary row in the admin tag list, plus the values to
// preserve when its rename form failed validation.
type tagRow struct {
	store.Tag
	FormName  string
	FormColor string
	Error     string
}

type tagsPageData struct {
	Admin store.Admin
	Umami config.Umami
	CSRF  string
	Tags  []tagRow

	Flash   string
	Error   string
	IsOwner bool

	// Create-form values, preserved after a 422.
	FormName  string
	FormColor string
}

// handleTags lists the vocabulary with live and total counts.
func (a *Admin) handleTags(rw http.ResponseWriter, r *http.Request) {
	data, err := a.tagPage(r, tagFormState{})
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusOK, "admin-tags", data)
}

// tagFormState carries the submitted values of the form that failed.
type tagFormState struct {
	ID    int64
	Name  string
	Color string
	Error string
}

func (a *Admin) tagPage(r *http.Request, form tagFormState) (tagsPageData, error) {
	tags, err := a.store.ListAdminTags(r.Context())
	if err != nil {
		return tagsPageData{}, err
	}
	session := stateFrom(r)
	data := tagsPageData{
		Admin:   session.Admin,
		Umami:   a.settings.Current(),
		CSRF:    session.Session.CSRFToken,
		Flash:   tagFlash(r),
		IsOwner: session.Admin.IsOwner(),
	}
	for _, tag := range tags {
		row := tagRow{Tag: tag, FormName: tag.Name, FormColor: tag.Color}
		if form.ID == tag.ID {
			row.FormName = form.Name
			row.FormColor = form.Color
			row.Error = form.Error
		}
		data.Tags = append(data.Tags, row)
	}
	if form.ID == 0 && form.Error != "" {
		data.Error = form.Error
		data.FormName = form.Name
		data.FormColor = form.Color
	}
	return data, nil
}

func (a *Admin) handleTagCreate(rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	color := strings.ToLower(strings.TrimSpace(r.FormValue("color")))
	if err := store.ValidateTag(name, color); err != nil {
		a.renderTagError(rw, r, tagFormState{Name: name, Color: color, Error: capitalize(err.Error())})
		return
	}
	created, err := a.store.CreateTag(r.Context(), name, color)
	switch {
	case errors.Is(err, store.ErrTagExists):
		a.renderTagError(rw, r, tagFormState{Name: name, Color: color, Error: capitalize(err.Error())})
		return
	case err != nil:
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "tag.create", strconv.FormatInt(created.ID, 10), "name="+created.Name)
	http.Redirect(rw, r, "/admin/tags?flash=tag-created", http.StatusSeeOther)
}

func (a *Admin) handleTagUpdate(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid tag id", http.StatusBadRequest)
		return
	}
	existing, err := a.store.GetTag(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	color := strings.ToLower(strings.TrimSpace(r.FormValue("color")))
	if err := store.ValidateTag(name, color); err != nil {
		a.renderTagError(rw, r, tagFormState{ID: id, Name: name, Color: color, Error: capitalize(err.Error())})
		return
	}
	updated, err := a.store.UpdateTag(r.Context(), id, name, color)
	switch {
	case errors.Is(err, store.ErrTagExists):
		a.renderTagError(rw, r, tagFormState{ID: id, Name: name, Color: color, Error: capitalize(err.Error())})
		return
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(rw, r)
		return
	case err != nil:
		a.internalError(rw, err)
		return
	}
	subject := strconv.FormatInt(id, 10)
	if existing.Name != updated.Name {
		a.auditAs(r, "tag.rename", subject, fmt.Sprintf("old=%s new=%s", existing.Name, updated.Name))
	}
	if existing.Color != updated.Color {
		a.auditAs(r, "tag.color", subject, fmt.Sprintf("old=%s new=%s", existing.Color, updated.Color))
	}
	http.Redirect(rw, r, "/admin/tags?flash=tag-saved", http.StatusSeeOther)
}

func (a *Admin) handleTagDelete(rw http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(rw, "invalid tag id", http.StatusBadRequest)
		return
	}
	existing, err := a.store.GetTag(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(rw, r)
		return
	}
	if err != nil {
		a.internalError(rw, err)
		return
	}
	if _, err = a.store.DeleteTag(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(rw, r)
			return
		}
		a.internalError(rw, err)
		return
	}
	a.auditAs(r, "tag.delete", strconv.FormatInt(id, 10), "name="+existing.Name)
	http.Redirect(rw, r, "/admin/tags?flash=tag-deleted", http.StatusSeeOther)
}

func (a *Admin) renderTagError(rw http.ResponseWriter, r *http.Request, state tagFormState) {
	data, err := a.tagPage(r, state)
	if err != nil {
		a.internalError(rw, err)
		return
	}
	a.render(rw, http.StatusUnprocessableEntity, "admin-tags", data)
}

func tagFlash(r *http.Request) string {
	switch r.URL.Query().Get("flash") {
	case "tag-created":
		return "Tag created."
	case "tag-saved":
		return "Tag saved."
	case "tag-deleted":
		return "Tag deleted."
	}
	return ""
}

// tagOptions lists every tag for the edit form, marking the ids the entry
// currently carries (or the submitted set when re-rendering an error).
func (a *Admin) tagOptions(ctx context.Context, selected []int64) ([]tagOption, error) {
	tags, err := a.store.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	chosen := make(map[int64]bool, len(selected))
	for _, id := range selected {
		chosen[id] = true
	}
	options := make([]tagOption, 0, len(tags))
	for _, tag := range tags {
		options = append(options, tagOption{
			ID:       tag.ID,
			Name:     tag.Name,
			Slug:     tag.Slug,
			Color:    tag.Color,
			Selected: chosen[tag.ID],
		})
	}
	return options, nil
}

// parseTagIDs reads the tag checkboxes of the multipart edit form. Malformed
// and non-positive ids are ignored; unknown ids are dropped by the store.
func parseTagIDs(raw []string) []int64 {
	ids := make([]int64, 0, len(raw))
	for _, value := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
