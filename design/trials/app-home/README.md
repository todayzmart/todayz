# Customer app home trial: bright (D-248)

The customer app home, using colour the way Zomato and Swiggy do. It is a standalone page, not the live app, and nothing in `customer/`, `shared/ui` or the tokens changes.

## Open it

Run `pnpm --filter @todayz/tokens gen` once, so the fonts and `tokens.css` exist. Then serve the repo root and open `/design/trials/app-home/index.html`. For example:

```bash
python3 -m http.server 5390 --bind 127.0.0.1
```

Then open http://127.0.0.1:5390/design/trials/app-home/index.html. It is phone-width; on a wide screen it sits centred like a phone.

## What it borrows from Zomato and Swiggy

- **Light grey page, white cards.** Colour comes from photos and promo banners, not from tinted backgrounds.
- **One loud brand colour, used sparingly.** That is our green. Cashback offer lines use a blue, and ratings get a green pill.
- **Full-strength promo banners** (green, plum, blue) with white text and a photo that fades in from the right.
- **Letter-spaced centred section labels** between hairlines ("WHAT ARE YOU LOOKING FOR?").
- **Round category photos**, a product rail with an `ADD` button over the photo, and big photo shop cards with the cashback written on the photo.
- **Sticky search with a mic button**, scrolling filter chips, a floating cart bar and a five-tab bottom bar.

We keep our fonts (Anek for headings and prices, now at weight 800; Figtree for text), Lucide icons and brand green. We copy none of their logos, illustrations, exact colours or layouts. Their orange and red fail contrast for small text (Swiggy `#fc8019` is 2.55:1 on white, Zomato `#e23744` is 4.32:1), so text stays on our green.

## Copy

Taken from the real app wherever it exists:

- search placeholder;
- the stats strip;
- radius chips;
- "Shops near you", "Show more shops";
- Open/Closed;
- "{pct}% cashback";
- Pickup/Delivery;
- "Confirmed {time} ago" (D-101);
- the tab names.

Cashback reads "as credits on every order", which matches 01 §8 (each store sets 0–20%, paid as credits on collected, delivered or POS sales).

New lines that a product owner should approve before any port:

- banner copy: "Reserve it. Walk over.", "Up to 20% back", "Chargers in stock nearby";
- "No cashback at this shop" on a 0% store;
- the "Rating 4.0+" filter chip.

The shops, prices, distances and wallet balance are made-up examples.

## Trial-only values

| Variable | Value | Use |
|---|---|---|
| `--page` | `#f4f5f7` | page background behind the cards |
| `--hair` | `#e8e8e8` | card and chip borders |
| `--rate` | `#1f7a3d` | rating pill |
| `--offer` | `#1d5bc0` | "% off" tag and cashback line |
| `--promo-green` / `--promo-plum` / `--promo-blue` | `#067a3c` / `#6b2a8c` / `#1d4ed8` | promo banners, white text |

If the founder keeps this look, these become tokens in `design/source/tokens.json`, and the screens change in `customer/` with message keys and real data.

## Photos

These are Openverse CC BY 2.0 photos in `../photos/`, credited in `../photos/CREDITS.md`. They are for the trial only; live screens use the shop's own photos.

## Checks

The page was checked at 360 px and 1280 px wide:

- axe (WCAG 2.2 AA plus best practice): 0 violations;
- no sideways scroll;
- no text under 11 px;
- no tap target under 24 px.
