## ADDED Requirements

### Requirement: Ledger navigation returns a complete document
The ledger handler SHALL return a complete HTML document for `GET /entries` when the request carries no `HX-Request` header, and SHALL return the `ledger` fragment when it does. Sort, pagination and cleared-search links SHALL keep working as plain navigations to that document.

#### Scenario: Sort link followed directly
- **WHEN** a client requests `/entries?sort=word&order=asc` without an `HX-Request` header
- **THEN** the response is a complete document with `<html>`, `<head>`, the stylesheet links, the masthead, the search toolbar and the ledger table

#### Scenario: HTMX requests the fragment
- **WHEN** a client requests the same URL with `HX-Request: true`
- **THEN** the response body is the `ledger` section alone, with no surrounding document

#### Scenario: JavaScript is disabled
- **WHEN** the ledger page is used with scripting disabled and a pager link is followed
- **THEN** the browser navigates to the pager URL and receives a rendered ledger page with a working toolbar

### Requirement: Dialog controls resolve on every page that shows the masthead
Every page that renders the shared `masthead` partial SHALL also render the `add-dialog` and `info-dialog` elements, and every element carrying `data-dialog-open` SHALL name a dialog id that exists in the same document. Each trigger SHALL keep an `href` that reaches the same content without scripting.

#### Scenario: Add entry from the statistics page
- **WHEN** a visitor activates "Add entry" or the floating add control on `/stats`
- **THEN** the add dialog opens in place and the browser does not navigate to `/?add=1`

#### Scenario: Story link from the statistics page
- **WHEN** a visitor activates "The story" on `/stats`
- **THEN** the info dialog opens in place and the browser does not navigate to `/?info=1`

#### Scenario: No dangling trigger
- **WHEN** any page containing `data-dialog-open` is parsed
- **THEN** the selector in every such attribute resolves to an element in that page

### Requirement: Skip link and main landmark
Every public and admin page SHALL start its body with a skip link targeting the page's `main` element, and that `main` element SHALL carry `id="content"` and `tabindex="-1"` so the link moves focus rather than only the viewport.

#### Scenario: First keyboard stop
- **WHEN** a keyboard user presses Tab on page load
- **THEN** the first focusable element is the skip link, and activating it focuses the `main` element

#### Scenario: One main landmark per page
- **WHEN** any full document served by the web or admin package is parsed
- **THEN** it contains exactly one `main` element and that element has `id="content"`

### Requirement: Toggle and dialog state is exposed
The theme toggle SHALL carry `aria-pressed` reflecting the applied theme, with `true` meaning dark, and SHALL update it whenever the theme changes. Each dialog trigger SHALL carry `aria-haspopup="dialog"`, `aria-controls` naming its dialog, and `aria-expanded` reflecting whether that dialog is open.

#### Scenario: Theme toggle state follows the theme
- **WHEN** the resolved theme is dark
- **THEN** the theme toggle has `aria-pressed="true"`, and after a toggle to light it has `aria-pressed="false"`

#### Scenario: Dialog closes by keyboard
- **WHEN** a dialog opened from a trigger is dismissed with Escape
- **THEN** the trigger's `aria-expanded` returns to `false`

### Requirement: Charts expose their values as text
Each bar chart on `/stats` SHALL provide a visually hidden table of the same values, and the decorative bar markup SHALL be hidden from assistive technology.

#### Scenario: Year chart text alternative
- **WHEN** the year chart is rendered
- **THEN** a visually hidden table lists each year with its entry count, and the visible bar list is `aria-hidden="true"`

#### Scenario: Person chart text alternative
- **WHEN** the person chart is rendered
- **THEN** a visually hidden table lists each name with its count, and the visible bar list is `aria-hidden="true"`

### Requirement: Admin forms reference hints and announce errors
Every hinted input in the admin login, setup and edit forms SHALL reference its hint element through `aria-describedby`, and validation or authentication errors SHALL be announced through an element with `role="alert"` that the form references through `aria-describedby`.

#### Scenario: Edit form field hints
- **WHEN** `/admin/entries/{id}/edit` is rendered
- **THEN** every input with a hint has an `aria-describedby` naming that hint's id, while the visible labels stay in place

#### Scenario: Failed sign-in is announced
- **WHEN** a login attempt fails
- **THEN** the error message carries `role="alert"` and the form references it through `aria-describedby`
