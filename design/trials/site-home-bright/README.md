# Site home trial: bright (D-248)

The same page as `../site-home/` (the cream and navy trial, D-247), with the same sections, copy and behaviour, re-skinned the way Zomato and Swiggy use colour. Compare the two side by side.

## Open it

Run `pnpm --filter @todayz/tokens gen` once. Then serve the repo root (`python3 -m http.server 5390 --bind 127.0.0.1`) and open http://127.0.0.1:5390/design/trials/site-home-bright/index.html.

## What changed from the cream and navy trial

All the changes are in one extra `<style>` block at the end of `<head>`. The only other edits are:

- the shelf cards render a photo instead of an icon on a tint;
- the store owners panel has the class `plum`.

| Part | Cream and navy | Bright |
|---|---|---|
| Page | cream `#faf9f5` with a dot grid | light grey `#f4f5f7`, no dots; alternate sections are white |
| Hero | cream with a soft green glow | full street photo with a dark scrim; white headline, mint "searchable." |
| Cards | white with a hairline border | white, no border, soft shadow |
| Top bar | navy | brand dark green |
| Radar, savings calculator | navy blocks | deep green `#0e5a36` blocks |
| Store owners panel | navy | plum `#55267e` |
| Footer | navy | white with grey text |
| Buttons | ink, green on hover | brand green |
| Shelf cards | Lucide icon on a pastel tint | product photo |
| Display type | Anek 600 | Anek 800 |
| Centred section labels | green eyebrow | grey letter-spaced label between hairlines |

The open copy and product questions in `../site-home/README.md` apply here unchanged.

## Photos

These are Openverse CC BY 2.0 photos in `../photos/`, credited in `../photos/CREDITS.md`, for the trial only.

## Checks

The page was checked at 360 px and 1280 px wide:

- axe (WCAG 2.2 AA plus best practice): 0 violations;
- no sideways scroll;
- no text under 11 px;
- no tap target under 24 px.

Text on the hero photo sits on a scrim that is 62–78% black.
