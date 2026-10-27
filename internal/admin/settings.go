package admin

import (
	"net/http"

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/store"
)

type settingsData struct {
	Admin  store.Admin
	Umami  config.Umami
	Env    config.Umami
	CSRF   string
	Values config.Umami
	Error  string
	Saved  bool
}

func (a *Admin) handleSettings(rw http.ResponseWriter, r *http.Request) {
	overrides, err := a.settings.Overrides(r.Context())
	if err != nil {
		a.internalError(rw, err)
		return
	}
	state := stateFrom(r)
	a.render(rw, http.StatusOK, "admin-settings", settingsData{
		Admin:  state.Admin,
		Umami:  a.settings.Current(),
		Env:    a.settings.Env(),
		CSRF:   state.Session.CSRFToken,
		Values: overrides,
		Saved:  r.URL.Query().Get("saved") == "1",
	})
}

func (a *Admin) handleSaveSettings(rw http.ResponseWriter, r *http.Request) {
	values := config.Umami{
		URL:       r.FormValue("umami_url"),
		WebsiteID: r.FormValue("umami_website_id"),
		Domains:   r.FormValue("umami_domains"),
	}
	if err := a.settings.Save(r.Context(), values); err != nil {
		state := stateFrom(r)
		a.render(rw, http.StatusUnprocessableEntity, "admin-settings", settingsData{
			Admin:  state.Admin,
			Umami:  a.settings.Current(),
			Env:    a.settings.Env(),
			CSRF:   state.Session.CSRFToken,
			Values: values,
			Error:  capitalize(err.Error()),
		})
		return
	}
	http.Redirect(rw, r, "/admin/settings?saved=1", http.StatusSeeOther)
}
