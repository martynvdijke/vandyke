## 1. Flutter Project Setup (`mobile/`)

- [ ] 1.1 Scaffold Flutter Android project in `mobile/` (`xyz.vandijke.vandyke`) with LANCo editorial ledger typography/themes, `flutter_riverpod`, `dio`, and `flutter_secure_storage`.
- [ ] 1.2 Implement server URL configuration screen.

## 2. Ledger Search, Stats & Misspelling Submission

- [ ] 2.1 Implement Ledger Feed with real-time search, sort order toggle, and pagination.
- [ ] 2.2 Implement Statistics Header/Tab showing total misspellings and frequency breakdown.
- [ ] 2.3 Implement "Log New Misspelling" bottom sheet submitting to the JSON REST API.
- [ ] 2.4 Add widget and unit tests in `mobile/test/`.

## 3. Non-Blocking CI & Release Integration

- [ ] 3.1 Create `.github/workflows/flutter-android.yaml` with `continue-on-error: true` and SHA-pinned actions.
- [ ] 3.2 Update `.releaserc.yaml` to bump `mobile/pubspec.yaml` and include it in `@semantic-release/git` `assets`.
- [ ] 3.3 Update `.github/workflows/release.yaml` with a non-blocking `android` job that uploads `vandyke-${VERSION}-android.apk` to the GitHub Release.
- [ ] 3.4 Verify workflows pass `actionlint`, `zizmor`, and `pinact run --check`.
