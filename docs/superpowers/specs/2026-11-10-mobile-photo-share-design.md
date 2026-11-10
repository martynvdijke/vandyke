# Mobile photo capture, sharing and deep links

Date: 2026-11-10

## Summary

The Android client in `mobile/` can now attach photo evidence to a logged
misspelling, upload it with the rest of the note, share an entry's permanent
link, and open `/e/{id}` links directly on the matching entry — including from
a cold start. The change adds capability only: the Riverpod providers, theme
and editorial layout are unchanged.

## Picker limits and downscale

`image_picker` is asked for `maxWidth: 2400`, `maxHeight: 2400` and
`imageQuality: 85`, so the picker returns an already-downscaled JPEG. Nothing
is upscaled: an image already inside 2400 px is passed through within the same
limits. No image-manipulation package is added.

- Camera capture requires the `CAMERA` permission, requested at runtime before
  the picker opens; a denial or a cancelled pick is a no-op for the attachment
  and the text fields.
- The preview renders the picked file itself, so it shows the bytes that will
  be uploaded (a picker re-encode may drop EXIF orientation).
- Dimensions are chosen to stay inside the server's 10 MiB, 12,000 px per side
  and 40 MP limits. A source that survives scaling is handled as a 413.

## Multipart contract

`POST /api/v1/entries` accepts the same field names in both forms:

| Part      | JSON field | Notes                                  |
| --------- | ---------- | -------------------------------------- |
| `word`    | `word`     | trimmed, required                      |
| `meaning` | `meaning`  | trimmed, required                      |
| `person`  | `person`   | omitted when empty                     |
| `photo`   | —          | JPEG file part, `image/jpeg`           |

`ApiClient.createEntry` keeps the JSON path when no photo path is supplied and
switches to `multipart/form-data` when one is. `ProgressMultipartRequest`
wraps the finalized body stream and reports bytes written against
`contentLength`, which drives the sheet's determinate progress bar. The submit
button stays disabled until the request ends.

Failure mapping reuses `ApiException`:

| Status | Behaviour                                                 |
| ------ | --------------------------------------------------------- |
| 413    | `isPayloadTooLarge`; "image is too large", text kept       |
| 422    | server validation message                                  |
| 429    | `Retry-After` seconds in `friendlyMessage`                 |
| 409    | "already in the ledger"                                    |

## Permalinks

`{server}/e/{id}` is built from the configured server address by
`entryPermalink`, using the normalized base URL. It is shared through
`share_plus` from the entry detail sheet and from the **Share** action offered
on the post-add confirmation. A dismissed share sheet is silent.

## Deep-link rules

`app_links` supplies both the cold-start link (`getInitialLink`) and the
running-app stream (`uriLinkStream`). Parsing accepts:

- `https://<host>/e/<id>`
- `vandyke://e/<id>` and `vandyke:///e/<id>`

Anything else — a different path, a missing id, a non-numeric or non-positive
id — is ignored. A cold-start link is held until the server address has been
loaded from secure storage and the app reaches the ready state.

Before resolving, the link's host (and port) is compared with the configured
server. A mismatch shows a "different ledger" notice and opens nothing, so the
user's session is never pointed at a foreign host. The custom scheme can only
be produced for this app, so it is always accepted.

A linked entry is fetched with `GET /api/v1/entries/{id}` rather than looked up
in the loaded page; `404` shows the existing not-found message, other failures
offer a retry for the same id. Entries opened from the feed or the latest-slips
strip are passed in already loaded, so those taps make no extra request.

## Android manifest

- `CAMERA` permission, requested at runtime.
- `https` intent filter for the public host `vandyke.thor.edu` with path prefix
  `/e/`, `android:autoVerify="false"` until an asset-links file exists. Path
  matching requires a host, so a self-hosted domain adds its own `android:host`
  entry.
- `vandyke` scheme intent filter with host `e` for tester links:
  `adb shell am start -a android.intent.action.VIEW -d "vandyke://e/42"`.

`minSdk` 24 and `compileSdk` 36 (the Flutter defaults already used by the
project) satisfy `image_picker`, `share_plus`, `app_links` and
`permission_handler`; no SDK bump was needed.

## Tests

Unit and widget tests cover the multipart builder (parts, field names, content
type, progress, JSON fallback), the 413/429 mapping, `entryById`, permalink
parsing and host rejection, and photo attach/preview/remove plus the share
action in the sheets. Device-only paths — a real camera capture, a slow
network, a genuine 413 and a cold-start intent — remain a manual check.
