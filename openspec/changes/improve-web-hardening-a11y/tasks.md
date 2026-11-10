## 1. Ledger Page As A Document

- [x] 1.1 Extract the head, opening body, masthead and closing markup of `base.html` into `page-head` and `page-foot` templates, leaving `base` as the home page with hero, statistics, recent list, toolbar and ledger. (The toolbar was also extracted as `ledger-toolbar` so `base` and `ledger-page` share one form instead of duplicating it; its `action` comes from `.ToolbarAction`.)
- [x] 1.2 Add a `ledger-page` template that wraps the toolbar and the `ledger` partial in `page-head` and `page-foot`, and render the add and info dialogs inside it. (Dialogs come from `page-foot`, which every `page-head` document uses.)
- [x] 1.3 Change `handleLedger` to render `ledger-page` when `HX-Request` is absent and `ledger` when it is present.
- [x] 1.4 Confirm the POST `/entries` response path still renders only the `ledger` fragment.
- [x] 1.5 Verify sort, pager and "Clear search" links resolve to the full document with JavaScript disabled. (The links already point at `/entries…`; `TestLedgerDocumentAndFragment` asserts the document form and the fragment form.)

## 2. Dialogs On Every Masthead Page

- [x] 2.1 Add the dialog fields (`ShowAdd`, `ShowInfo`, `FormError`, `FormWord`, `FormMeaning`, `FormPerson`) and a `Ledger ledgerData` to `statsPageData`, populated with the zero query.
- [x] 2.2 Render `add-dialog` and `info-dialog` in `stats.html` alongside the shared `masthead`.
- [x] 2.3 Audit every `data-dialog-open` attribute and confirm its selector resolves on `/`, `/entries` and `/stats`. (`TestDialogTriggersResolveOnMastheadPages` parses every attribute on those three pages.)

## 3. Accessibility Structure

- [x] 3.1 Add `class="skip-link"` anchors targeting `#content` as the first element in the body of `base.html` (via `page-head`), `stats.html`, `permalink.html` and `internal/admin/templates/base.html`. (The not-found document in `permalink.html` also got one, since it is a full document served by the web package.)
- [x] 3.2 Add `id="content"` and `tabindex="-1"` to the `main` element on each of those documents.
- [x] 3.3 Add `.skip-link` and `.visually-hidden` rules to `app.css` and `admin.css`, matching the existing `.sr-only` behaviour in `admin.css`.
- [x] 3.4 Add `aria-haspopup="dialog"`, `aria-controls` and `aria-expanded="false"` to every dialog trigger in `base.html` and `stats.html`. (Triggers in `masthead`, `page-foot` and the stats empty state.)
- [x] 3.5 Set `aria-pressed` on each `[data-theme-toggle]` in `applyTheme`, and on initial DOMContentLoaded, with `true` meaning dark.
- [x] 3.6 Set `aria-expanded` to `true` when a dialog opens and back to `false` on its `close` event.
- [x] 3.7 Mark the year and person bar lists `aria-hidden="true"` and add a visually hidden table with a caption and two columns for each chart.

## 4. Admin Forms

- [x] 4.1 Add `aria-describedby` to hinted inputs in `edit.html`, `login.html` and `setup.html` and give each hint element an id. (Login has no hints, so only its error region was wired.)
- [x] 4.2 Add `aria-describedby` on the login and setup forms pointing at their error region, keeping `role="alert"` on the message.
- [x] 4.3 Add `aria-describedby` to the photo file input in `edit.html` pointing at its hint, and confirm the remove-photo checkbox keeps its visible label.

## 5. Discoverability Routes

- [x] 5.1 Add `Store.AllEntryIDs` returning the id of every entry ordered by id. (Filters `deleted_at IS NULL` so the sitemap obeys the deletion guarantee; this is the only store change and no existing query was touched.)
- [x] 5.2 Add `GET /robots.txt` returning `text/plain` with a disallow for `/admin` and an absolute sitemap line built from `absoluteURL`.
- [x] 5.3 Add `GET /sitemap.xml` returning `application/xml` with `/`, `/stats` and every `/e/{id}` as absolute locations.
- [x] 5.4 Add `GET /site.webmanifest` returning `application/manifest+json` with name, short name, start URL, display, theme colour `#f4eee1`, background colour `#f4eee1` and the two existing icons.
- [x] 5.5 Add `<link rel="manifest" href="/site.webmanifest">` to `base.html`, `ledger-page`, `stats.html`, `permalink.html` and `internal/admin/templates/base.html`. (Also the not-found document in `permalink.html`.)

## 6. Documentation

- [x] 6.1 Add a README section listing `/robots.txt`, `/sitemap.xml`, `/site.webmanifest` and the accessibility behaviour of the ledger page.
- [x] 6.2 Add `docs/superpowers/specs/2026-11-10-web-hardening-a11y-design.md` describing the full-document ledger route, the dialog placement rule, the accessibility changes and the discoverability routes.
- [x] 6.3 Link the new spec from `docs/index.md`.

## 7. Testing

- [x] 7.1 Add a web test asserting `/entries` without `HX-Request` returns a document containing `<html>` and the masthead, and with the header returns only the ledger section. (`TestLedgerDocumentAndFragment`.)
- [x] 7.2 Add a test asserting `/stats` contains both dialog ids and that every `data-dialog-open` value resolves to an element in the response. (`TestDialogTriggersResolveOnMastheadPages` covers `/`, `/entries` and `/stats`.)
- [x] 7.3 Add a test asserting one `main#content` per document, a skip link preceding it, and a manifest link in the head, on `/`, `/entries`, `/stats`, `/e/{id}` and `/admin/login`. (`TestPublicDocumentsExposeSkipLinkMainAndManifest` and `TestAdminDocumentsExposeStructureManifestAndFormAria`, the latter also covering `/admin`.)
- [x] 7.4 Add a test asserting the year and person chart tables have the same row count as the visible bar lists. (`TestStatsChartsExposeTableAlternatives`.)
- [x] 7.5 Add tests for `/robots.txt`, `/sitemap.xml` and `/site.webmanifest` covering content type, required fields and the `PUBLIC_BASE_URL` override. (`TestDiscoverabilityRoutes`, `TestDiscoverabilityRoutesCarrySecurityHeaders` and `TestDiscoverabilityHonoursPublicBaseURL`; the first also covers icons resolving and a deleted entry leaving the sitemap.)
- [x] 7.6 Run `go test ./...` and `go vet ./...`.
