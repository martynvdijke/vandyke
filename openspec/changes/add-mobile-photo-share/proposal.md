## Why

The web add form posts `multipart/form-data`, so a sighting can be recorded with a photo, but the Android client sends JSON only: `createEntry` in `mobile/lib/src/api/api_client.dart` posts `word`, `meaning` and `person` to `POST /api/v1/entries` and has no image path. `mobile/pubspec.yaml` carries no picker, share or deep-link dependency, so a misspelling photographed in the field is logged as text without its evidence. The detail sheet renders the photo referenced by `photo_url` but offers no way to share the `/e/{id}` permalink the server already publishes, and the app has no handler for incoming `/e/{id}` links.

## What Changes

- Add camera capture and gallery selection to the log-misspelling sheet through `image_picker`, with a removable preview before submission.
- Downscale the chosen image on the device to at most 2400 px on the longest side at JPEG quality 85 before upload.
- Send the photo as the `photo` part of a `multipart/form-data` request to `POST /api/v1/entries`, keeping the JSON body when no photo is attached.
- Report upload progress and route 413, 422, 429 and 409 responses through the `ApiException` type.
- Add a share action to the entry detail sheet and to the confirmation shown after a successful add, sharing the `{server}/e/{id}` permalink built from the configured server address.
- Handle `/e/{id}` deep links through `app_links`, including cold start, and resolve the entry with `GET /api/v1/entries/{id}` instead of reusing the list payload.
- Correct the `mobile/README.md` claim about `GET /api/v1/entries/{id}`, or make the client call it; this change makes the client call it.

### Non-goals

- Offline queueing or local caching of entries and photos.
- An iOS build or an App Store target.
- Admin features in the app.
- Editing or deleting entries from the app.

## Capabilities

### New Capabilities

- `mobile-photo-capture`: Camera capture, gallery selection, client-side downscale and multipart photo upload from the Android client.
- `mobile-deep-links`: Permalink sharing and `/e/{id}` deep-link handling that opens an entry resolved by id.

## Impact

- `mobile/pubspec.yaml`: adds `image_picker`, `share_plus` and `app_links`.
- `mobile/lib/src/api/api_client.dart`: multipart submission and `GET /api/v1/entries/{id}`.
- `mobile/lib/src/widgets/log_misspelling_sheet.dart`: photo attachment and upload progress.
- `mobile/lib/src/widgets/entry_detail_sheet.dart`: share action.
- `mobile/lib/src/app.dart`: deep-link subscription and entry-sheet routing.
- `mobile/android/app/src/main/AndroidManifest.xml`: `https` and custom-scheme intent filters for `/e/{id}`.
- `mobile/README.md`: permalink and deep-link behaviour.
