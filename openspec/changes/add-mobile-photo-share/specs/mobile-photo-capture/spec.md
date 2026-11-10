## ADDED Requirements

### Requirement: Photo attachment from camera or gallery
The log-misspelling sheet SHALL let the user attach one photo, either by capturing it with the device camera or by selecting it from the gallery, and SHALL allow the attachment to be removed or replaced before submission.

#### Scenario: Capture a photo with the camera
- **WHEN** the user chooses the camera action in the log-misspelling sheet and confirms a capture
- **THEN** the sheet shows a preview of the captured image and enables submission with that image attached

#### Scenario: Choose a photo from the gallery
- **WHEN** the user chooses the gallery action and selects an image
- **THEN** the sheet shows a preview of the selected image in place of any previous attachment

#### Scenario: Remove the attachment
- **WHEN** the user removes the attached photo before submitting
- **THEN** the sheet returns to a text-only submission and the photo is not uploaded

### Requirement: Client-side downscale before upload
The client SHALL downscale an attached image to at most 2400 px on its longest side and encode it as JPEG at quality 85 before upload, so a normal phone photo is not rejected by the server's 10 MiB and 12,000 px limits.

#### Scenario: Large camera photo is reduced
- **WHEN** the user attaches a camera photo whose longest side exceeds 2400 px
- **THEN** the bytes handed to the API client have a longest side of at most 2400 px and an `image/jpeg` content type

#### Scenario: Small image is passed through
- **WHEN** the user attaches an image already within 2400 px on its longest side
- **THEN** the client still submits it as a JPEG within the same limits without upscaling it

### Requirement: Multipart submission with optional photo
The client SHALL submit a new entry with an attached photo as `multipart/form-data` to `POST /api/v1/entries` using the fields `word`, `meaning`, `person` and `photo`, and SHALL keep the existing JSON body when no photo is attached.

#### Scenario: Submit with a photo
- **WHEN** the user submits the sheet with text fields and an attached photo
- **THEN** the client posts `multipart/form-data` to `POST /api/v1/entries` with `word`, `meaning`, the optional `person` and the `photo` file part

#### Scenario: Submit without a photo
- **WHEN** the user submits the sheet with no attachment
- **THEN** the client posts the existing JSON body and the server response is handled as it is today

### Requirement: Upload progress during photo submission
The sheet SHALL show determinate upload progress while a request carrying a photo is in flight, and SHALL prevent a second submission of the same entry until the first request completes or fails.

#### Scenario: Progress while uploading
- **WHEN** a multipart request with a photo is being sent
- **THEN** the sheet displays the proportion of the request body written and disables the submit action

#### Scenario: Progress ends on failure
- **WHEN** the upload fails before completion
- **THEN** the progress indicator is removed and the error message replaces it

### Requirement: Typed handling of upload failures
The client SHALL map 413, 422, 429 and 409 responses from `POST /api/v1/entries` to a readable message, using the existing `ApiException` fields and honouring `Retry-After` for 429.

#### Scenario: Image exceeds the server limit
- **WHEN** the server rejects the upload with 413 and the `request_too_large` error
- **THEN** the sheet reports that the image is too large and keeps the entered text so the user can resubmit without it

#### Scenario: Rate limit during upload
- **WHEN** the server responds with 429 and a `Retry-After` header
- **THEN** the sheet states how long to wait before retrying

#### Scenario: Duplicate entry
- **WHEN** the server responds with 409
- **THEN** the sheet reports that the exact entry already exists in the ledger
