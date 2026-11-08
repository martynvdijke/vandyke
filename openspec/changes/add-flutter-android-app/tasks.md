## 1. Flutter Project Setup (`mobile/`)

- [x] 1.1 Scaffold Flutter Android project in `mobile/` (`xyz.vandijke.vandyke`) with LANCo editorial ledger typography/themes, `flutter_riverpod`, and `flutter_secure_storage`. (Deviation: uses the `http` package instead of `dio`; transport is a thin injectable `ApiClient` over `http.Client`.)
- [x] 1.2 Implement server URL configuration screen.

## 2. Ledger Search, Stats & Misspelling Submission

- [x] 2.1 Implement Ledger Feed with real-time search, sort order toggle, and pagination.
- [x] 2.2 Implement Statistics Header/Tab showing total misspellings and frequency breakdown. (Deviation: `/api/v1/stats` exposes only total, unique words, last-30-day count and first/last entry dates, so the tab shows those aggregates; no frequency breakdown is available without a new API endpoint.)
- [x] 2.3 Implement "Log New Misspelling" bottom sheet submitting to the JSON REST API. (Includes the optional `person` field.)
- [x] 2.4 Add widget and unit tests in `mobile/test/`. (19 tests: API client, models, widget.)

## 3. Non-Blocking CI & Release Integration

- [x] 3.1 Create `.github/workflows/flutter-android.yaml` with `continue-on-error: true` and SHA-pinned actions.
- [x] 3.2 Update `.releaserc.yaml` to bump `mobile/pubspec.yaml` and include it in `@semantic-release/git` `assets`.
- [x] 3.3 Update `.github/workflows/release.yaml` with a non-blocking `android` job that uploads `vandyke-${VERSION}-android.apk` to the GitHub Release.
- [x] 3.4 Verify workflows pass `actionlint`, `zizmor`, and `pinact run --check`. (actionlint and pinact pass; `zizmor` is not installed in this environment, so that check was skipped.)
