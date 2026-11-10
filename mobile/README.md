# The vanDyke Ledger — Android

The pocket client for the LANCo misspelling ledger. It talks to the existing
JSON REST API (`/api/v1/entries`, `/api/v1/entries/{id}`, `/api/v1/persons`,
`/api/v1/stats`) and lets you search the record, read the counters, log a new
misspelling from the field, attach photo evidence, and share the permanent
`/e/{id}` link for any entry.

## Run

```sh
flutter pub get
flutter run
```

The first launch asks for the ledger address (defaults to
`https://vandyke.thor.edu`) and validates it against `/api/v1/stats` before
saving it in Android's secure storage.

## Logging a misspelling

The open-book button on the ledger opens the field-note sheet. Fill in the
misspelling and the correction it should have been, optionally attribute it to
a person, and optionally attach one photo:

- **Camera** takes a new photo (Android asks for camera access the first time;
  a denied permission leaves the sheet and its text untouched).
- **Gallery** selects an existing photo and replaces any current attachment.
- The chosen image is downscaled on the device to at most **2400 px on its
  longest side** and re-encoded as **JPEG at quality 85** before it is sent, so
  a normal phone photo stays inside the server's 10 MiB, 12,000 px and 40 MP
  limits. The preview shows the exact image that will be uploaded; **×**
  removes it and returns the sheet to a text-only submission.

With a photo attached the client posts `multipart/form-data` to
`POST /api/v1/entries` with the same fields as the JSON body plus the file:

| Part     | Value                                              |
| -------- | -------------------------------------------------- |
| `word`   | the misspelling                                    |
| `meaning`| the correction or context note                    |
| `person` | optional attribution                              |
| `photo`  | the JPEG, `image/jpeg`, at most 2400 px per side   |

Without a photo the existing JSON body is used. While a photo upload is in
flight the sheet shows determinate byte progress and disables the submit
button; the text fields survive a failure so the note can be resubmitted
without the image. Failures are typed: **413** reads as "image is too large",
**422** shows the server's validation message, **429** states how long to wait
from `Retry-After`, and **409** reports that the exact entry is already in the
ledger.

## Permalinks and sharing

Every entry has a permanent `{server}/e/{id}` link. The share action in the
entry detail sheet (and the **Share** action on the confirmation after a
successful add) opens the platform share sheet with that absolute URL, built
from the configured server address. A dismissed share sheet is not an error.

## Deep links

The app answers `/e/{id}` links through `app_links`, both when it is already
running and when the link launches it (the link is held until the stored
server address has loaded). The linked entry is resolved with
`GET /api/v1/entries/{id}`, so a link works even when the entry is not on the
page currently in memory. A `404` shows the app's not-found message; any other
failure offers **Try again**.

A link whose host does not match the configured ledger is refused with a
"different ledger" notice instead of sending the session to another server.

Two intent filters make a link testable without a public host:

- `https://vandyke.thor.edu/e/<id>` — the public host, path prefix `/e/`,
  `autoVerify` off until an asset-links file is published. A self-hosted
  domain needs its own `android:host` entry (path matching requires a host).
- `vandyke://e/<id>` — the tester scheme.

```sh
adb shell am start -a android.intent.action.VIEW -d "vandyke://e/42" \
  xyz.vandijke.vandyke
```

For a release build, substitute the release package (`xyz.vandijke.vandyke`);
debug builds install as `xyz.vandijke.vandyke.dev`.

## Build

```sh
flutter build apk --release
```

Release signing reads `android/key.properties` (see `key.properties.example`);
without it the build falls back to the debug key so local release builds and CI
still work. Debug builds install as `xyz.vandijke.vandyke.dev`.

## Test

```sh
flutter analyze
flutter test
```

The tests cover the API client contract with `MockClient` (including the
multipart photo part and the JSON path), the typed 413/422/429/409 failures,
`GET /api/v1/entries/{id}`, permalink parsing and host rejection, photo
attach/preview/remove in the field-note sheet, the share action, the editorial
theme, server setup, the ledger feed, and debounced search.
