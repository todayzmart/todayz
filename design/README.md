# Design system (source copy)

Live artifact (source of truth for visuals): https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF (v8).

- `source/` is a copy of the artifact's project files: `tokens.json`, `components/*/` (bundle.css, previews, READMEs), `patterns.md`, `localization.md`, logos.
- `generators/` holds the Python scripts that produced the previews. Order: `screens.py`, then `insights.py` (screens.py overwrites BusinessOverview), then `harness.py`.
- `shared/tokens` (Wave 0) generates CSS variables and TS constants from `source/tokens.json`. If the artifact changes, re-copy it here first.
