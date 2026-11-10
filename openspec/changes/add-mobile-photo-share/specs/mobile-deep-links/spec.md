## ADDED Requirements

### Requirement: Share an entry permalink
The app SHALL offer a share action on the entry detail sheet and on the confirmation shown after a successful add, sharing the absolute permalink `{server}/e/{id}` built from the configured server address.

#### Scenario: Share from the detail sheet
- **WHEN** the user taps share on an open entry
- **THEN** the platform share sheet opens with `{server}/e/{id}` for that entry

#### Scenario: Share after adding an entry
- **WHEN** a new entry is created successfully
- **THEN** the confirmation offers the same share action for the new entry's permalink

### Requirement: Deep-link handling for entry permalinks
The app SHALL handle `/e/{id}` links through `app_links`, opening the entry detail sheet for the linked id both when the app is already running and when the link starts the app.

#### Scenario: Link opens a running app
- **WHEN** an `/e/{id}` link arrives while the app is in the foreground or background
- **THEN** the app opens the detail sheet for that entry id on top of the current screen

#### Scenario: Link starts a stopped app
- **WHEN** the app is launched from an `/e/{id}` link
- **THEN** after the stored server address is loaded the app opens the detail sheet for that entry id

### Requirement: Entry resolution by id
The app SHALL resolve a linked entry with `GET /api/v1/entries/{id}` rather than from the currently loaded list page, and `mobile/README.md` SHALL describe that call accurately.

#### Scenario: Entry is not on the loaded page
- **WHEN** a link points to an entry outside the page currently held in memory
- **THEN** the app fetches `GET /api/v1/entries/{id}` and renders that entry in the detail sheet

#### Scenario: Entry does not exist
- **WHEN** `GET /api/v1/entries/{id}` returns 404
- **THEN** the app shows the existing not-found message and leaves the current screen usable

### Requirement: Reject links for another server
The app SHALL compare the host of an incoming `/e/{id}` link with the configured server address and SHALL not request the entry from a different host.

#### Scenario: Link belongs to another instance
- **WHEN** an `/e/{id}` link arrives whose host does not match the configured server address
- **THEN** the app shows a notice that the link belongs to a different ledger and does not open a detail sheet

#### Scenario: Resolving a link fails on the network
- **WHEN** the entry request fails for a reason other than 404
- **THEN** the detail sheet shows a retry action for the same entry id
