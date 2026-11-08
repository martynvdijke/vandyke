# The vanDyke Ledger — Android

The pocket client for the LANCo misspelling ledger. It talks to the existing
JSON REST API (`/api/v1/entries`, `/api/v1/entries/{id}`, `/api/v1/stats`) and
lets you search the record, read the counters, and log a new misspelling from
the field.

## Run

```sh
flutter pub get
flutter run
```

The first launch asks for the ledger address (defaults to
`https://vandyke.thor.edu`) and validates it against `/api/v1/stats` before
saving it in Android's secure storage.

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

The tests cover the API client contract with `MockClient`, payload parsing, the
editorial theme, server setup, the ledger feed, and debounced search.
