# D-259 Profile photo: QA report

A signed-in customer can add, change and remove a profile photo. It shows on Account (C-18) and in the Home header avatar (C-03), right away and only to them.

- Branch: `claude/angry-bose-8759f6`. Not pushed; the lead merges.
- Everything ran locally: the shared test Postgres, `storagetest.Fake` for the bucket, and Playwright against a faked API. Nothing touched a cloud account, and the running local stack was left alone.
- Decision: D-259 in `docs/plan/12-decision-log.md`. Rules: 01 §2 (Profile photo) and §17 (`profile_photo_uploads_per_hour`). Screens: 06 C-03 and C-18.

## Founder answers

| Question | Answer | Effect |
|---|---|---|
| Moderation | Show right away | No review queue; confirm makes it the photo at once |
| Who sees it | Only the customer | Private bucket only; `photoUrl` (presigned GET, 15 min) only in the customer app's `GET /v1/me` |

## What was built

### Backend

- **Migration** `49901_w4-99_profile_photo.sql`:
  - `users.photo_key` (nullable);
  - `user_photo_uploads` (`uploading | confirmed | expired`);
  - the setting `profile_photo_uploads_per_hour` (10, from 1 to 60).
- **Contract** in `backend/api/paths/me.yaml` and `schemas/me.yaml`:
  - `POST /v1/me/photo/uploads` returns a presigned PUT with `Cache-Control: no-store`;
  - `POST /v1/me/photo/uploads/{uploadId}/confirm` returns the user with `photoUrl` and a new ETag;
  - `DELETE /v1/me/photo`;
  - `Me.photoUrl`.
- **`internal/identity/photo.go`**:
  - type and size are checked at registration;
  - a per-user advisory lock and a rolling-hour count produce `429 rate_limited`;
  - confirm reads the raw file and cleans it, then swaps `photo_key` in one transaction. It then removes the raw file and the previous photo on a best-effort basis;
  - remove clears the key and deletes the file;
  - account deletion removes every object the user's upload rows name;
  - an upload nobody confirmed is marked `expired` after an hour, at the next registration.
- **`internal/platform/imageclean`** (new):
  - decodes JPEG, PNG or WebP;
  - refuses more than 40 MP before decoding;
  - applies the EXIF orientation and scales to at most 512 px a side;
  - flattens transparency onto white and re-encodes as a JPEG with no metadata.
- **Authz:** policy `me.photo.manage` (customers only), with the golden file updated.

### Customer app

- **`src/account/photo.ts`**:
  - `useProfilePhoto()` checks the type, then shrinks the photo in the browser (at most 1024 px, JPEG 0.85, EXIF orientation applied), then checks the 5 MB limit;
  - it registers the upload, PUTs with only `Content-Type`, then confirms;
  - it updates `['me']`, then reloads it so the ETag that the name form needs is current.
- **`src/account/ProfilePhoto.tsx`**:
  - `PhotoImage` falls back to the initial when the signed link no longer loads;
  - `PhotoAvatar` has a camera badge, a hidden file input and a spinner;
  - With a photo, the camera badge opens a sheet: "Choose a new photo" or "Remove photo". The card has no remove button (founder, 26 Oct: "dont keep remove photo").
  - `PhotoActions` has a polite status line and an error line.
- **Wiring:**
  - `routes/account.tsx` keeps the photo state in `SignedIn`, so it outlives the Profile remount after a reload;
  - `home/Header.tsx` shows the photo in the avatar link.
- **i18n:** 10 `account.photo.*` keys in `customer/auth.*.json`. English is final. Hindi, Kannada, Tamil and Telugu are imported machine drafts and wait for review.

## Tests run

| Check | Result |
|---|---|
| `go test -race` identity, imageclean, settings, authz, storage, settingslint | pass |
| `go test ./cmd/todayz` (incl. `profile_photo_http_test.go` and the contract validator) | pass (104 s) |
| golangci-lint, gofmt | 0 issues |
| customer eslint (`--max-warnings 0`), tsc, stylelint | clean |
| customer vitest | 216/216, including 8 in `profile-photo.test.tsx` |
| customer `build:e2e` + precache check | ok |
| customer `pnpm size` | 145.44 KB initial JS (budget 170 KB) |
| i18n lint | 0 errors |
| Playwright `e2e/profile-photo.spec.ts` at 360 px | pass |

The HTTP test covers:

- a cashier gets 403;
- a GIF and an over-5-MB file get 422;
- registration returns `no-store` and the signed headers;
- another customer confirming the upload gets 404;
- the owner's confirm returns an ETag and `photoUrl`;
- the business app's `/v1/me` has no `photoUrl`;
- DELETE returns `photoUrl: null`;
- nothing is written to the public images bucket.

The unit tests cover:

- the upload and its exact PUT headers;
- the type and size refusals before any call;
- the 429, 422 and storage-403 messages;
- remove;
- the broken-link fallback;
- the header avatar.

`make check` was not run as a whole: it would run `make gen` and the full suite on the 16 GB Mac beside the running stack. The backend and customer checks above were run instead.

## Screenshots (360 px)

- `customer-c18-photo-initial-360.png`: initial and camera badge
- `customer-c18-photo-refused-360.png`: a GIF refused
- `customer-c18-photo-set-360.png`: the photo on the card, no remove button
- `customer-c18-photo-sheet-360.png`: the badge's sheet, "Choose a new photo" and "Remove photo"
- `customer-c03-header-photo-360.png`: the photo in the Home header

## Risks and follow-ups

- **Number collisions.** Migration `49901` and decision D-259 were the next free ones on this branch. If another branch takes either before this merges, renumber at merge.
- **Re-signed links.** `photoUrl` changes on every `/v1/me` read, so the browser downloads the photo again after each reload (about 30–60 KB). A stable cache key, such as a CDN path signed per user, can come later if it matters.
- **Orphans.** A raw upload never confirmed stays in the bucket until account deletion. A bucket lifecycle rule on `profile/*.upload` (for example, delete after 1 day) would clean these up. It belongs in `infra/` and is the lead's call.
- **Translations.** The Hindi, Kannada, Tamil and Telugu strings are machine drafts; they ship only after review.
- **Design branch.** The design branch is reworking the Account top card. `PhotoAvatar` and `PhotoActions` are self-contained, so the merge only has to place them.
