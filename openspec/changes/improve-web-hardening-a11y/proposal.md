## Why

`GET /entries` renders only the `ledger` partial (`internal/web/web.go:289`), so the sort links (`internal/web/templates/partials.html:14`), the pager links (`partials.html:39`) and "Clear search" (`partials.html:58`) return a bare `<section>` with no `<html>`, `<head>`, stylesheet, masthead or dialogs when followed directly or with JavaScript disabled. `/stats` reuses the `masthead` partial, whose "Add entry" and "The story" triggers target `#add-dialog` and `#info-dialog`, but only `base.html` renders those dialogs, so on `/stats` the buttons fall through to `/?add=1` and `/?info=1` (`internal/web/templates/stats.html:51`). No page has a skip link, the theme toggle reports no state, and there is no `robots.txt`, `sitemap.xml` or `site.webmanifest` route.

## What Changes

- Render a complete `ledger-page` document for non-HTMX `GET /entries`, keeping the `ledger` fragment for `HX-Request` so HTMX swaps behave as today.
- Render the add and info dialogs on every page that uses the shared `masthead` partial, and give the stats view model the dialog fields and `ledgerData` those dialogs read.
- Add a skip link and `id="content"` with `tabindex="-1"` on `<main>` in the public and admin layouts.
- Set `aria-pressed` on the theme toggle from the resolved theme, and `aria-haspopup`, `aria-controls` and `aria-expanded` on dialog triggers.
- Replace the visual-only year and person bar charts with visually hidden tables, and mark the decorative bars `aria-hidden`.
- Add hint and error associations to the admin login, setup and edit forms, where the templates currently carry no `aria-*` attributes.
- Add `GET /robots.txt`, `GET /sitemap.xml` and `GET /site.webmanifest`, and link the manifest from every page head.

### Non-goals

- A JavaScript framework or a build step.
- A service worker, offline cache or installable-app behaviour beyond the manifest.
- Any change to the visual design language, typography, colour or layout.
- Internationalisation.

## Capabilities

### New Capabilities

- `ledger-progressive-enhancement`: The ledger page works as a complete document without JavaScript, and every page exposes accessible structure and state.
- `site-discoverability`: Crawler directives, a sitemap and a web manifest served by the binary.

## Impact

- `internal/web/web.go`: ledger handler layout choice, new `robots.txt`, `sitemap.xml` and `site.webmanifest` handlers.
- `internal/web/templates/base.html`, `partials.html`, `dialogs.html`, `stats.html`, `permalink.html`.
- `internal/admin/templates/base.html`, `login.html`, `setup.html`, `edit.html`.
- `internal/web/static/js/app.js`: theme state and dialog expanded state.
- `internal/web/static/css/app.css`, `internal/web/static/css/admin.css`: skip-link and visually-hidden utilities.
- `internal/store/store.go`: a method returning every entry id for the sitemap.
