# Design artifact snapshot

A full local copy of the design artifact https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF, so the design system can be read and viewed without claude.ai.

Snapshot: published version `1792652339-d9fd`, taken 2026-10-22.

## What is where

| Artifact part | Local copy |
|---|---|
| `project/*` (tokens, components, patterns, localization, `design-system.json`) | `design/source/`, byte-identical; hashes in `SOURCE.sha256` |
| Asset store (logos) | `design/source/assets/Logos/` |
| `index.html` (page shell of the Design System type) | `design/artifact/index.html` |
| `SKILL.md` (the type's instructions) | `design/artifact/SKILL.md` |
| `artifact-type/*` (viewer runtime: `app.js`, `app.css`, `demo.json`, font, `reference/`, `run.html`) | `design/artifact/artifact-type/` |
| Comments | none on the artifact at snapshot time |
| Database | declared by the type, but no data stored at snapshot time; the page keeps its own state in `project/design-system.json` |

`index.html`, `SKILL.md` and `artifact-type/` belong to the Design System artifact type, not to TodayZ. They are kept only so the snapshot is complete. Do not edit them. Edit design content in the artifact, then re-copy it into `design/source/`.

Asset ids in the artifact's asset store:

| File | Asset id | Bytes |
|---|---|---|
| `todayz-logo.jpg` (primary) | `6f866fd7ada0910b36fbebf158698e5b` | 67987 |
| `todayz-logo-alt.jpg` | `2bd925a23a8f52cd4e24cee1d9fe9cf9` | 72036 |
| `todayz-bag-mark.png` | `6667b7f4a7c25de14e784e132dd9a335` | 94351 |

## Checking and refreshing

- To check this copy, run `shasum -a 256 -c MANIFEST.sha256` in this folder, and `shasum -a 256 -c ../artifact/SOURCE.sha256` in `design/source/`.
- To refresh:
  1. Ask Claude to read the artifact's file list and download every published file.
  2. Copy `project/` over `design/source/` and the rest into this folder.
  3. Regenerate both `.sha256` files and update the version above.
