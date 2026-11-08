## ADDED Requirements

### Requirement: Non-blocking Android CI workflow
The repository SHALL include `.github/workflows/flutter-android.yaml` running `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with `continue-on-error: true` and SHA-pinned actions.

#### Scenario: Pull request triggers Android CI
- **WHEN** a pull request is opened or updated
- **THEN** the `flutter-android` job runs analysis, tests, and APK build without blocking PR merge on failure

### Requirement: Semantic-release APK publishing
The release procedure (`.releaserc.yaml` and `.github/workflows/release.yaml`) SHALL bump `mobile/pubspec.yaml` to `${nextRelease.version}` and run a non-blocking `android` job that attaches `vandyke-<version>-android.apk` to the GitHub Release.

#### Scenario: New release is published on main
- **WHEN** `semantic-release` publishes a new release `vX.Y.Z`
- **THEN** the `android` job builds the release APK from `vX.Y.Z` and uploads `vandyke-X.Y.Z-android.apk` to the GitHub Release without failing `notify` or Docker publishing if an Android error occurs
