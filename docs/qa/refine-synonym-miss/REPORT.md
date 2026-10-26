# refineHint: the synonym-miss half (D-238)

Branch `claude/refine-synonym-miss-3aff6e`. Finishes the half of 01 §11.1 that D-231 left open.

## What was decided

A plain "the synonym table had no match" is not usable. `product_synonyms` holds about 70 generic
terms (milk, curd, ghee, atta, dal and their hi/kn/te/ta forms), so "maggi", "parle g", "surf excel"
or "colgate" all miss it while returning good results; the app would call the model on most
searches. I stopped and asked the lead, who chose option A (D-238; B, "also zero result stores",
is recorded there as rejected because it misses junk partial-match pages).

`refineHint` is now the parser half OR the synonym-miss half. The miss half is true when:

1. parsing is on (`parse` not false) and words remain after the chips (a query that was only
   chips is searched as typed with no miss hint);
2. the query is not a barcode and the request does not name one `productId`;
3. no phrase of 1–3 words in the query is a term or synonym in the table;
4. the search found no listing (full-text, trigram, or a category named by the query);
5. no active master product holds all the words (`CatalogueKnows`, using `ix_products_search`).

Tests 1–4 reuse what `find()` already computes. Test 5 is one small extra query, made only when
1–4 all passed (the unknown-words case). A product in the catalogue that no nearby shop stocks
("parle g" in the test) gets no hint: that is a stock gap, which "Ask shops nearby" serves.

## Changes

- `backend/internal/search/expand.go`: `expand` also returns whether any phrase hit the table.
- `backend/internal/search/search_run.go`: `find` returns a `synonymMiss{possible, tsq}`; `Search`
  makes the `CatalogueKnows` call when possible and sets `RefineHint`. `searchReq.skipMiss` is set
  by `refine` for chip-only queries.
- `backend/internal/search/refine.go`: comment now says it is the parser half; the chip-only case
  sets `skipMiss`.
- `backend/db/queries/search/search.sql`: `CatalogueKnows`.
- `basket.go`, `shop.go`: adapt to the new return values (no behaviour change).
- API description text (`api/paths/discover.yaml`, `api/schemas/discover.yaml`): wording only, no
  shape change.
- Docs: 01 §11.1, 04 §2 (search row), 07 W1-06 row, 11 AI-C2 and the `refine_ai_min_words`
  row, 12 D-231 note and new **D-238**.

No new setting, no migration, no response shape change.

## Tests

`TestRefineSynonymMiss` (`refine_miss_db_test.go`): synonym hit with and without a result
(`dahi`, `doodh`, `paneer`) → no hint; "maggi" (stocked), "Maggi Masala Noodles" (exact master
name), "parle g" (catalogue only), "nandini" (brand with stock), and "maggi" under a price cap
that empties the page → no hint; "kuch meetha chahiye" and "sasta" (unrecognised) → hint; the
same with `parse=false` → none; unknown and known barcodes → none; "kuch meetha under 33" → chip
applied and hint; "open now" (chips only) → none; punctuation only → none; the existing parser
half ("curd from the corner shop" → hint, with `parse=false` none, "fresh curd" none).
`TestRefineChips` and `TestRefineWithoutReader` are unchanged and pass. `perf_test.go` records a
miss search and checks `CatalogueKnows` walks `ix_products_search`.

Run (shared test Postgres, `go test -p 2`):

```
go test -p 2 ./internal/search/... ./internal/ai/querynorm/...   ok (search, querynorm, querynormeval)
golangci-lint run ./internal/search/... ./internal/ai/querynorm/...   0 issues
go build ./...  gofmt  go vet                                        clean
```

`make gen` here needed the main checkout's `.tools/bin` on PATH (not installed in this
worktree). Backend and contract steps pass (`apigen check`: 322 pairs); the last step,
`gen-frontend` (tokens), fails here only because the fontsource packages are not installed in
this worktree.

## For the lead

- `make testpg-up` in a worktree with no `.cache/testpg.url` runs `docker rm -f todayz-testpg`
  first (`scripts/testpg.sh up`). I ran it, it removed the shared server other sessions were
  using, and the `docker run` after it failed on an image I/O error. A few minutes later the
  container was up again (someone recreated it) with a new password. If another session's DB
  tests failed with connection errors around 08:35–08:37 IST on 26 Oct, that was me. I then wrote
  a worktree-local URL file from the running container's environment (never printed). Worth
  making `testpg.sh up` reuse a running container when only the URL file is missing.
- The customer app's use of `refineHint` needs no change (same boolean). Expect it to be true
  more often than before for unrecognised queries of any length, including one-word ones.
