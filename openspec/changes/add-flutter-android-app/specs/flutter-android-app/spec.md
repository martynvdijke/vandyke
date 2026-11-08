## ADDED Requirements

### Requirement: Ledger browsing, search, and statistics
The Flutter Android app SHALL display misspelling ledger entries with search, sorting, pagination, and aggregate statistics from the vanDyke JSON REST API.

#### Scenario: Search for an existing misspelling
- **WHEN** the user types a spelling variant into the search bar
- **THEN** the app queries the server API and displays matching ledger records and occurrence counts

### Requirement: Quick misspelling submission
The app SHALL let users submit a newly spotted misspelling and optional context note to the server API.

#### Scenario: Log a new misspelling from mobile
- **WHEN** the user submits a new misspelling via the quick-add sheet
- **THEN** the app sends a `POST` request to the ledger API and refreshes the entry list and stats counter
