# Product image import: full run on the local stack (26 Oct 2026)

`todayz images import --limit 20000` against the local stack (`make dev` database and MinIO), after applying migration 49601 to it. No cloud calls. The catalogue has 2,994 live products: 2,909 with a brand and 1,935 with a barcode, but only 2 of those barcodes are real (Amul Butter, Maggi). The other 1,933 are the seed's in-store `29…` codes.

## Run 1 (18:57–19:03 IST, stopped by hand)

| Outcome | Count |
|---|---|
| Tried | 137 |
| Found | 2, both wrong (see below) |
| Not found | 8 |
| Medicines skipped | 1 |
| Nothing to look up | 84 |
| Failed | 41: Open Food Facts answered 503 to almost every name search, Open Beauty Facts 500 |

I stopped it after 6 minutes. Open Food Facts' 503 page says it is under "unusually high demand" and that requests from anonymous users are limited. Even one search every 15 seconds was refused 3 times out of 4. Barcode lookups still answered.

The two finds were both wrong, which showed two bugs:

- **Rajdhani Whole wheat atta 5 kg** matched barcode `2900000010192`, which is "Karaneige caramel 200g" (France) on Open Food Facts. A `2…` code is a shop's own in-store code and means a different product in every shop.
- **India Gate Basmati rice 1 kg** matched "Brown Rice India gate 1 kg" by name. The "kg" in "1 kg" counted as a matching word.

## Fixes

- In-store and other restricted barcodes (GS1 `2…`, `02`, `04`, coupons `980–984` and `99`, EAN-8 `0…` and `2…`) are no longer looked up. A product with only such a code falls back to the name-and-brand search, if it has a brand.
- A unit standing alone ("1 kg", "500 ml") no longer counts as a word of the name.
- A 503 now stops the run, like a 429, instead of calling again every 6.5 seconds.

## Run 2 (after the fixes)

It stopped on the first product: the first name search was answered 503, so 0 found.

## State of the local database

There are 3 imported drafts, all `pending`:

| Product | Match | Verdict |
|---|---|---|
| Amul Butter 100 g | barcode 8901262010016 | Right (smoke run, checked by eye) |
| Rajdhani Whole wheat atta 5 kg | barcode 2900000010192 | **Wrong: reject in A-08** |
| India Gate Basmati rice 1 kg | name | **Wrong (brown rice): reject in A-08** |

## Next

- Run again when Open Food Facts search is answering, with the same command. Imported products are skipped, so a rerun starts where this one stopped.
- Name search will stay fragile. Open Food Facts asks bots to use its data export, and Run 1 showed the cost of a loose match. Better sources of real coverage are real barcodes in shop catalogues, or a lookup against the Open Food Facts export instead of its search.
