## Context

The client in `mobile/` is a Flutter Android app whose transport is a thin `ApiClient` over `package:http`. It talks to a self-hosted vanDyke instance whose address the user enters at first launch and which is validated against `GET /api/v1/stats`. The server already accepts `multipart/form-data` on `POST /api/v1/entries` with `word`, `meaning`, `person` and an optional `photo`, enforces 10 MiB, 12,000 px per side and 40 MP limits, and returns typed errors for oversized bodies, validation failures, rate limiting and duplicates. The client already maps 422, 429 and 409; 413 arrives as a plain status code today.

## Goals / Non-Goals

**Goals:**
- Log a misspelling with photographic evidence from the phone in one sheet.
- Keep uploads inside the server limits without adding an image codec dependency.
- Share the canonical `/e/{id}` permalink from the app.
- Open `/e/{id}` links directly on the matching entry, including from a cold start.

**Non-Goals:**
- Offline queueing, retry scheduling or a local cache.
- An iOS build.
- Admin features, entry editing or deletion.
- Changing the server API, its limits or its error codes.

## Decisions

1. **Reuse the existing multipart endpoint**: `POST /api/v1/entries` already accepts the same field names as the JSON body plus `photo`, so the client adds a multipart branch rather than a new upload route or a two-step create-then-attach flow.

2. **Downscale inside the picker**: `image_picker` accepts `maxWidth`, `maxHeight` and `imageQuality`, so the client requests at most 2400 px on the longest side at quality 85 and receives an already-compressed JPEG. This avoids adding an image manipulation package and keeps the typical phone photo under the 10 MiB limit.

3. **Streamed multipart for progress**: `http.MultipartRequest` does not expose upload progress, so `api_client.dart` gains a small `ProgressMultipartRequest` subclass that wraps the request body stream and reports bytes written. `createEntry` takes an optional photo path and an optional progress callback, and keeps the JSON path when no photo is present.

4. **Resolve permalinks by id**: the detail sheet accepts an entry id and calls `GET /api/v1/entries/{id}` so a deep link does not depend on the entry being present in the currently loaded page. A 404 shows the existing not-found message; other failures show a retry panel.

5. **One deep-link package**: `app_links` is used for both the initial link (`getInitialLink`) and the running-app stream, which covers cold start and warm start with a single dependency. Android intent filters cover `https://<host>/e/<id>` on the configured host and the custom scheme `vandyke://e/<id>`.

6. **Host check before routing**: a link whose host does not match the configured server address is not opened against that server. The app shows a short notice and leaves the current screen in place, which avoids sending the user's session to a foreign host.

7. **Share through the platform sheet**: `share_plus` shares the plain `{server}/e/{id}` URL so the receiving app decides how to render it. The same action is attached to the detail sheet and to the post-add confirmation.

## Risks / Trade-offs

- **[Picker output still too large]** -> A source image with extreme dimensions can survive the picker's scaling; the 413 response is mapped to a readable "image is too large" message and the sheet keeps the text fields so the user can resubmit without the photo.
- **[EXIF orientation lost]** -> Re-encoding through the picker may drop orientation metadata; the preview shows the exact bytes that will be uploaded before submission.
- **[Deep link to an entry on another instance]** -> The host check rejects the link with a notice rather than switching servers or showing a misleading 404.
- **[Upload progress on slow links]** -> Progress is byte-based, not time-based, so a stalled connection shows no movement; the existing request timeout still terminates the call.
- **[Cold-start ordering]** -> The initial link can arrive before the server address is loaded from secure storage; the link is held until setup is complete and then routed.
