## Why

The vanDyke Corrections Ledger (`vandyke`) tracks creative misspellings of the vanDyke name in the wild with a JSON REST API. Spotting a new misspelling on a coffee cup, badge, or email happens on the go, making a pocket Flutter Android app the fastest way to search the ledger and log new entries.

## What Changes

- Add a Flutter Android application under `mobile/` (`xyz.vandijke.vandyke`) consuming the existing JSON REST API (`entries`, `stats`, search/sort/pagination).
- Implement Ledger Search & Pagination, Statistics Overview, and 1-tap "Log New Misspelling" submission sheet.
- Add a non-blocking `.github/workflows/flutter-android.yaml` workflow and integrate APK building/publishing into `.releaserc.yaml` and `.github/workflows/release.yaml`.

### Non-goals

- Modifying the existing Go JSON REST API or rate-limiter behavior.
- Blocking backend or Docker releases if the Android build fails.

## Capabilities

### New Capabilities

- `flutter-android-app`: Native Flutter Android pocket client for the vanDyke Corrections Ledger.
- `android-ci-release`: Non-blocking Flutter Android CI workflow and semantic-release GitHub Release APK attachment.

### Modified Capabilities

- None.

## Impact

- New `mobile/` Flutter project directory.
- New `.github/workflows/flutter-android.yaml` workflow.
- Updates to `.releaserc.yaml` and `.github/workflows/release.yaml`.
