# Web hardening and accessibility

**Date:** 2026-11-10
**Status:** Implemented
**Change:** `openspec/changes/improve-web-hardening-a11y`

## Purpose

Two defects and a set of gaps are closed here:

1. `GET /entries` rendered only the `ledger` fragment, so following a sort,
   pager or "Clear search" link directly (or with JavaScript disabled) produced
   a bare `<section>` with no head, stylesheet or navigation.
2. `/stats` reuses the shared `masthead`, whose "Add entry" and "The story"
   triggers target `#add-dialog` and `#info-dialog`, but the dialogs were only
   rendered by `base.html`. On `/stats` the triggers fell through to
   `/?add=1` / `/?info=1`.

Alongside those, the site gains a skip link and main landmark, exposed toggle
and dialog state, text alternatives for its charts, ARIA wiring on the admin
forms, and the crawler/browser metadata routes (`robots.txt`, `sitemap.xml`,
`site.webmanifest`).

## Full-document ledger route

`handleLedger` branches on `HX-Request`:

- header present → render the `ledger` fragment, unchanged, so HTMX swaps keep
  working;
- header absent → render a new `ledger-page` document.

`base.html` is split into three reusable templates:

| Template | Contents |
| --- | --- |
| `page-head` | doctype, head (including the manifest link), skip link, grain, masthead |
| `page-foot` | footer, floating add button, both dialogs, toasts, HTMX script |
| `ledger-toolbar` | the search/filter form, whose `action` comes from `.ToolbarAction` |

`base` keeps the home hero, statistics and recent list around `ledger-toolbar`
and `ledger`; `ledger-page` places the same toolbar and ledger inside
`page-head` / `page-foot`. Both links in the ledger already carried valid
`href` values, so the full-document route makes them correct instead of adding a
second navigation mechanism. The toolbar posts to `/` on the home page and to
`/entries` on the ledger page.

`POST /entries` still returns the bare `ledger` fragment for HTMX requests; the
no-JS path still redirects to `/?added=1`.

## Dialog placement rule

The add and info dialogs are part of `page-foot` and are rendered explicitly by
`stats.html`. The rule is: **wherever the shared `masthead` partial is used, both
dialogs are rendered in the same document.** `permalink.html` keeps its reduced
inline masthead, which has no dialog triggers, so it needs no dialogs.

`statsPageData` therefore gains the dialog fields (`ShowAdd`, `ShowInfo`,
`FormError`, `FormWord`, `FormMeaning`, `FormPerson`) and a zero-valued
`Ledger ledgerData`, which is all the dialog templates read. A test parses every
`data-dialog-open` value on `/`, `/entries` and `/stats` and asserts the matching
`id` exists.

## Accessibility

- **Skip link and main landmark:** every public and admin document starts its
  body with `<a class="skip-link" href="#content">` and its `<main>` carries
  `id="content" tabindex="-1"`, so activating the link moves focus, not just the
  viewport.
- **Theme state:** the toggle ships `aria-pressed="false"`. `applyTheme` in
  `app.js` sets it from the theme it applies (true = dark) and is called again on
  `DOMContentLoaded` because the script runs in `<head>` before the buttons are
  parsed.
- **Dialog state:** every trigger carries `aria-haspopup="dialog"`,
  `aria-controls` and `aria-expanded="false"`. Opening a dialog sets its triggers
  to `true`; the dialog's `close` event sets them back, which also covers Escape
  and backdrop dismissal.
- **Charts:** the year and person bar lists are `aria-hidden="true"` and each
  panel gains a `.visually-hidden` `<table>` with a caption and two columns
  (year/count, name/count), generated from the same view-model slices.
- **Admin forms:** each hinted input in `login.html`, `setup.html` and
  `edit.html` names its hint via `aria-describedby`. Error paragraphs keep
  `role="alert"` and gain an id; the form names it with a conditional
  `aria-describedby` so a failed submission is announced with the form.

`.skip-link` and `.visually-hidden` are added to `app.css` and `admin.css`;
`admin.css` already had the equivalent `.sr-only` rule.

## Discoverability routes

Registered in `Web.Register` beside the existing routes, so they inherit the
shared security middleware (`X-Frame-Options: DENY`, CSP, `nosniff`):

- `GET /robots.txt` — `text/plain`; `Disallow: /admin` and an absolute
  `Sitemap:` line.
- `GET /sitemap.xml` — `application/xml`; home, `/stats` and one `/e/{id}` per
  live entry. Built with `Store.AllEntryIDs` (ids only, `deleted_at IS NULL`) and
  every location passed through `absoluteURL`, so `PUBLIC_BASE_URL` wins and the
  request host is the fallback, matching canonical links. No `lastmod` is emitted
  because `entries` has no modification column.
- `GET /site.webmanifest` — `application/manifest+json`; name, short name,
  `start_url` `/`, `standalone`, `#f4eee1` theme and background colours, and the
  existing `favicon.svg` (`any`) and `apple-touch-icon.png` (180×180).

The manifest is linked from every document head (`page-head`, `stats.html`,
`permalink.html`, the not-found page and `internal/admin/templates/base.html`).

## Trade-offs

- The sitemap grows with the ledger and is generated per request; it is cheap
  because only ids are read.
- Head markup for the public pages lives in one `page-head`, but `stats.html`,
  `permalink.html` and the admin base still own their own heads, so a new head
  link is added in four places. Tests fetch every route and assert the manifest
  link is present.
- `aria-pressed` and `aria-expanded` start `false` and are corrected as soon as
  `app.js` runs; without JavaScript neither control can change state, so the
  starting value is truthful.

## Testing

- `/entries` returns a document without `HX-Request` and the bare section with it.
- Dialog triggers resolve on `/`, `/entries` and `/stats`.
- One `main#content`, a preceding skip link and a manifest link on `/`,
  `/entries`, `/stats`, `/e/{id}`, `/admin/login` and `/admin`.
- Year and person hidden tables have the same row count as their bar lists.
- `robots.txt`, `sitemap.xml` and `site.webmanifest` return the right content
  types and fields, icons resolve, deleted entries leave the sitemap,
  `PUBLIC_BASE_URL` overrides the URLs, and the routes carry the security headers.
- A failed sign-in exposes `role="alert"` plus `aria-describedby`, and every
  edit-form hint is referenced by its input.
