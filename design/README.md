# Design system (source copy)

Live artifact (source of truth for visuals): https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF (v8, published version `1792652339-d9fd`).

- `source/` is a copy of the artifact's project files: `tokens.json`, `components/*/` (bundle.css, previews, READMEs), `patterns.md`, `localization.md`, logos.
- `generators/` holds the Python scripts that produced the previews. Order: `screens.py`, then `insights.py` (screens.py overwrites BusinessOverview), then `harness.py`.
- `shared/tokens` (Wave 0) generates CSS variables and TS constants from `source/tokens.json`. If the artifact changes, re-copy it here first.
- `artifact/` is a full snapshot of the rest of the artifact: page shell, the Design System type's viewer runtime, asset ids and sha256 manifests. See `artifact/README.md`.

## Where the plan differs from the design

`source/` stays a byte-identical copy of the artifact, so the differences are recorded here.

- **DemandInsights (D-99).** The "Wanted nearby" row drops the nearby-shop count: the product never shows how many shops stock an item and never names one. It shows the search count, plus "No shop nearby has it." when at least 10 other live verified shops of distinct businesses are in the radius and none lists the item. The product adds seven kinds to the design's four, and a "Not useful" action on each row (not on "Expired on shelf"). See `docs/plan/01-product-rules.md` §12.
