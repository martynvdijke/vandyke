## 1. Dependencies and platform configuration

- [x] 1.1 Add `image_picker`, `share_plus` and `app_links` to `mobile/pubspec.yaml` and resolve them with `flutter pub get`. (also declares `http_parser` — already transitive via `http` — for the `image/jpeg` part type, and `permission_handler` for 1.3)
- [x] 1.2 Confirm the minimum Android SDK and compile SDK in `mobile/android/app/build.gradle.kts` satisfy the three new plugins, recording any bump in the change notes. (Flutter defaults `minSdk = 24`, `compileSdk = 36`; no bump needed)
- [x] 1.3 Add the `CAMERA` permission to `mobile/android/app/src/main/AndroidManifest.xml` and request it at runtime before capture. (`permission_handler` requests it; the request is injected through `cameraPermissionProvider` so a denial is testable)
- [x] 1.4 Add intent filters for `https://<host>/e/<id>` and `vandyke://e/<id>` to the main activity, with `android:autoVerify` disabled until an asset-links file exists. (the `https` filter names the public host `vandyke.thor.edu`, since Android ignores a path without a host; a self-hosted domain adds its own `android:host`)

## 2. Photo selection and downscale

- [x] 2.1 Add photo state to `mobile/lib/src/widgets/log_misspelling_sheet.dart`: a nullable attachment, a preview, and remove and replace actions. (replace = picking again from either source)
- [x] 2.2 Wire the camera action to `ImagePicker.pickImage` with `source: ImageSource.camera`, `maxWidth: 2400`, `maxHeight: 2400` and `imageQuality: 85`.
- [x] 2.3 Wire the gallery action to the same picker with `source: ImageSource.gallery` and the same limits.
- [x] 2.4 Handle a cancelled picker selection and a denied camera permission without changing the current attachment or clearing the text fields.

## 3. Multipart upload and progress

- [x] 3.1 Extend `createEntry` in `mobile/lib/src/api/api_client.dart` with an optional photo path and an optional upload-progress callback.
- [x] 3.2 Add a `ProgressMultipartRequest` that wraps the body stream and reports bytes written, and use it from `createEntry` when a photo is present.
- [x] 3.3 Post `word`, `meaning`, the optional `person` and the `photo` part to `POST /api/v1/entries` with `multipart/form-data`, leaving the JSON path untouched when there is no photo.
- [x] 3.4 Render determinate progress in the sheet from the callback and disable the submit action while the request is in flight.
- [x] 3.5 Map 413 to a dedicated "image is too large" message and confirm 422, 429 and 409 still flow through `ApiException.friendlyMessage`. (added `ApiException.isPayloadTooLarge` and a 413 fallback message; 422/429/409 unchanged)

## 4. Permalink sharing

- [x] 4.1 Add a share action to `mobile/lib/src/widgets/entry_detail_sheet.dart` that builds `{server}/e/{id}` from the configured server address.
- [x] 4.2 Share that URL through `share_plus` and handle a dismissed share sheet without an error.
- [x] 4.3 Add the same share action to the confirmation state shown after a successful add, using the created entry's id. (a **Share** action on the post-add snackbar)

## 5. Deep links and entry resolution

- [x] 5.1 Add `app_links` subscription in `mobile/lib/src/app.dart`, consuming the initial link and the running-app link stream. (`EntryLinkSource` wraps `getInitialLink` + `uriLinkStream`)
- [x] 5.2 Parse `/e/{id}` from both the `https` and `vandyke` forms and hold an initial link until the stored server address has loaded.
- [x] 5.3 Compare the link host with the configured server host and show the different-ledger notice when they differ. (host and port compared for `http(s)`; the `vandyke` scheme is always local)
- [x] 5.4 Add `entryById(int id)` to `mobile/lib/src/api/api_client.dart` calling `GET /api/v1/entries/{id}`.
- [x] 5.5 Make the detail sheet accept an entry id, resolve it with `entryById`, and show the not-found message on 404 and a retry action on other failures.
- [x] 5.6 Let the ledger list pass an already-loaded entry to the detail sheet so opening an entry from the list does not repeat the request.

## 6. Documentation

- [x] 6.1 Update `mobile/README.md` to describe photo upload, the `multipart/form-data` fields, permalink sharing and `/e/{id}` deep links, and correct the `GET /api/v1/entries/{id}` description to match the implemented call.
- [x] 6.2 Add `docs/superpowers/specs/2026-11-10-mobile-photo-share-design.md` summarising the picker limits, multipart contract and deep-link rules.
- [x] 6.3 Link the new spec from `docs/index.md`.
- [x] 6.4 Note the Android intent filters and the `vandyke://` scheme in the README so a tester can trigger a link without a public host.

## 7. Testing

- [x] 7.1 Add a unit test for the multipart request builder asserting the parts, field names, content type and that the JSON path is used when no photo is attached.
- [x] 7.2 Add a unit test that a 413 response maps to the image-too-large message and that 429 still reads `Retry-After`.
- [x] 7.3 Add a widget test for attaching, previewing and removing a photo in the log-misspelling sheet. (plus cancelled-picker and denied-permission cases)
- [x] 7.4 Add a widget test for the share action on the detail sheet asserting the shared URL is `{server}/e/{id}`.
- [x] 7.5 Add a test for deep-link parsing that covers an initial link, a running-app link, a foreign host and a non-numeric id. (parsing and host matching are pure functions, covered directly)
- [x] 7.6 Run `flutter analyze` and `flutter test` in `mobile/` and record the results in the change notes. (`flutter analyze`: No issues found! — `flutter test`: +61 All tests passed! on Flutter 3.47.7)
- [x] 7.7 Manually verify on a device or emulator: camera capture, gallery selection, an upload over a slow connection, a 413 by attaching an oversized image, and a cold-start `vandyke://e/<id>` link. (not performed: no device/emulator in this environment; the paths are covered by unit and widget tests with the picker, permission, HTTP client and link source injected)
