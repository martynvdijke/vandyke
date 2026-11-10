## Context

The public site is server-rendered `html/template` with HTMX layered on top. `base.html` is simultaneously the home page and the only document that renders the add and info dialogs, while `stats.html` and `permalink.html` are separate documents with their own `<head>` and inline mastheads. `handleLedger` (`internal/web/web.go:278`) always renders the `ledger` fragment at `web.go:289`, and `handleCreate` in the same file already branches on `HX-Request`, so the server has a reliable signal for fragment versus document.

## Goals / Non-Goals

**Goals:**
- Every ledger link and form control produces a usable page when requested directly, with JavaScript disabled, or by a crawler.
- Every page has one skip link, one `main` landmark, and reports toggle and dialog state to assistive technology.
- The two bar charts have a text alternative that states year, name and count.
- Crawler directives, a sitemap and a manifest are served by the same binary with no new dependency.

**Non-Goals:**
- Replacing HTMX or removing the fragment responses that HTMX depends on.
- A visual redesign of any page.
- Service-worker caching or offline support.
- Translating template copy.

## Decisions

1. **Full document for non-HTMX `/entries`**: `handleLedger` renders a new `ledger-page` template when `HX-Request` is absent and the existing `ledger` fragment when it is present. `base.html` is split into a `page-head` and `page-foot` pair so `ledger-page` shares the head, masthead, footer, dialogs and toolbar without re-declaring them, and `base` keeps the home hero, statistics and recent list. This is preferred over converting every ledger link to `hx-get`: the links already carry valid `href` values, so the full-document route makes those values correct instead of adding a second navigation mechanism to maintain, and it fixes direct navigation and crawler access in the same step.

2. **Dialogs follow the masthead**: the add and info dialogs are emitted by `base.html` today and read `.Ledger.Query`, `.ShowAdd`, `.ShowInfo` and the form fields. `statsPageData` gains those dialog fields plus a `Ledger ledgerData` carrying the zero query, and `stats.html` renders `add-dialog` and `info-dialog` after its footer. `permalink.html` keeps its reduced inline masthead, which has no dialog triggers, so it needs no dialogs. The rule is therefore: the shared `masthead` partial is always paired with both dialogs.

3. **Theme state lives in the client**: the resolved theme depends on `localStorage` and `prefers-color-scheme`, which the server cannot know. The toggle ships `aria-pressed="false"`; `applyTheme` in `app.js` sets `aria-pressed` from the value it just applied, on first run and on every change, where `true` means dark. The existing `aria-label` is kept as the accessible name.

4. **Dialog triggers announce expansion**: each `data-dialog-open` element gains `aria-haspopup="dialog"`, `aria-controls` pointing at the dialog id, and `aria-expanded="false"`. `app.js` sets it to `true` in the open handler and back to `false` on the dialog's `close` event, which also covers Escape and backdrop dismissal. Triggers are anchors with working `href` fallbacks, so the attributes describe behaviour rather than gate it.

5. **Charts get a table alternative**: the year and person bar lists become `aria-hidden="true"`, since their bars are decorative divs. Each panel gains a visually hidden `<table>` with a caption and two columns (year and count, name and count). A `.visually-hidden` utility is added to `app.css` and `admin.css`; `admin.css` already contains the equivalent `.sr-only` at `admin.css:427`, and the admin layout gains a `.skip-link` anchor of its own.

6. **Discoverability reuses the permalink URL builder**: `robots.txt`, `sitemap.xml` and `site.webmanifest` are plain `GET` handlers registered in `Web.Register` next to the existing routes. The sitemap uses `absoluteURL` (`internal/web/web.go:331`), so `PUBLIC_BASE_URL` wins and the request host is the fallback, matching canonical links. A new `Store.AllEntryIDs` returns ids only, keeping the sitemap cheap on a large ledger. `robots.txt` disallows `/admin` and names the sitemap; the manifest reuses `favicon.svg` (`sizes: any`) and `apple-touch-icon.png` (180×180), with `theme_color` and `background_color` set to `#f4eee1`, the light `--paper` token.

7. **Admin forms name their hints and errors**: the login, setup and edit templates already wrap inputs in `<label>`, but none of the three contains an `aria-*` attribute. Each hint gets an id and its input gets `aria-describedby`; the existing error paragraph keeps `role="alert"` and the enclosing form gains `aria-describedby` naming it, so a failed submission is announced with the form rather than only in the page.

## Risks / Trade-offs

- **[Duplicated chrome]** -> `base.html`, `stats.html`, `permalink.html` and `internal/admin/templates/base.html` each own a head, so a new head link is added in four places. The manifest link and skip link are added per document and covered by a test that fetches each route and asserts both are present.
- **[Sitemap size]** -> every permalink is listed, so the document grows with the ledger. Each entry contributes one id-and-loc pair, no `lastmod` is emitted because `entries` has no modification column, and the response is generated per request without caching state.
- **[State before script runs]** -> `aria-pressed` and `aria-expanded` start `false` and are corrected as soon as `app.js` runs, which is synchronous in `<head>`. A no-JS user sees the toggle as not pressed and dialog triggers as collapsed, which matches the fact that neither control can change state without script.
- **[Chart duplication]** -> the visible bars and the hidden table can drift if the view model changes. Both are generated from the same `.Years` and `.Persons` slices, and a test compares the table row count with the bar count.
