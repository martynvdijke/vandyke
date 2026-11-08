## Context

`vandyke` is a zero-dependency-binary Go (`net/http` + embedded SQLite) application that already exposes a complete JSON REST API for listing, searching, sorting, viewing stats, and submitting new misspelling entries.

## Goals / Non-Goals

**Goals:**
- Provide an instant-launch Flutter Android app in `mobile/` matching the LANCo editorial ledger aesthetic.
- Support searching, sorting, stats viewing, and 1-tap misspelling submission.
- Keep CI and release integration 100% non-blocking (`continue-on-error: true`).

**Non-Goals:**
- Offline database replication.

## Decisions

1. **Direct JSON REST Client**: Connect `dio` directly to the existing `/api/...` endpoints with graceful handling of `429 Too Many Requests` token-bucket responses.
2. **Non-Blocking CI & Release**: SHA-pin all GitHub Actions in `.github/workflows/flutter-android.yaml` and `.github/workflows/release.yaml`, set `continue-on-error: true`, and upload `vandyke-<version>-android.apk` to GitHub Releases.

## Risks / Trade-offs

- **[Rate Limit Handling]** -> Surface clear feedback when the per-IP token bucket returns HTTP 429.
