# Product events

One YAML file per domain (`<domain>.yaml`), owned by that domain's work package. `make gen` (`mk/26-events.mk`, `backend/internal/devtools/eventgen`) validates them and writes two git-ignored outputs:

- `backend/gen/events` (embedded copy, loaded by `eventcat.Load` for `POST /v1/events`)
- `shared/telemetry/src/gen/events.ts` (`ClientEventName`, `EventProps`, so `emit()` is typed)

Rules (05 §2.6, D-50, D-59):

- Event names are unique across all files, `snake_case`, past tense.
- Props are enums, numbers, booleans or ids. Free-text `string` props are rejected.
- `source: client` events may be posted by apps. `source: server` events are facts emitted by the API and are refused on `/v1/events`.
- Location is an H3 resolution-8 cell (`geoCell`), never coordinates.
- Nothing is sent from local dev or tests; the emitter is a no-op there.

See `app.yaml` for the shape.
