# MQ-08 Languages

Each of the five languages on the five main customer screens: Home, Search, Shop, Checkout and Tracking. The tester checks for clipped or overlapping text, the right font for the script, untranslated keys, and the format of numbers, ₹ amounts, distances and times.

- **Covers:** 08 §5 MQ-08; G3; `R-1.2`; SC-I18N manual side; CR-20 manual side.
- **Screens:** C-01, C-03, C-05, C-07, C-09, C-11, C-12, C-15, C-18.
- **Devices:** P360 (profile A). Repeat sections B to F once each on P390 for one Indic language of the tester's choice.
- **Time:** about 12 minutes per language.

## Preconditions

1. `make reset`; `clock set <today>T11:00:00+05:30`.
2. C11 (wallet 500) signed in on profile A, location QA point, radius 2 km.
3. A shop phone (profile B) signed in as K22 at Sri Lakshmi, to accept the order in each section.
4. Translations for hi, kn, te and ta are present in `shared/i18n` (keys flagged "machine draft" still count as present).

## What to check on every screen

| Check | Pass when |
|---|---|
| Clipping | No text cut off, overlapping, or pushed out of its button, chip, tab or card at 360 px width. Long words wrap; buttons grow in height, not width. |
| Font | The script renders in the font named in the section (DevTools → Elements → Computed → "Rendered fonts"). No boxes (tofu) and no serif fallback. |
| Keys | No raw i18n key (for example `checkout.place_order`) and no English left on a screen that has a translation. Product and shop names stay as written (`R-1.2`). |
| ₹ | Indian grouping: ₹1,240 and ₹1,24,000 style, ₹ before the number, paise only where the amount has paise (₹99.99). |
| Distance | One decimal under 10 km ("0.6 km"), whole numbers from 10 km. |
| Time | IST, 12-hour with am/pm in English; the language's own form in the others. Relative time "12 min ago" translated. |
| Digits | Record whether digits are 0–9 or the script's own digits. The plan does not say which (Q-34); they must be the same on every screen in one language. |

## Steps

### A. Switch language

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.01 | Open Account (C-18) → Language. | Five options, each in its own script: English, हिन्दी, ಕನ್ನಡ, తెలుగు, தமிழ். | `R-1.2` C-18 | ▢ | |
| MQ-08.02 | Pick हिन्दी and go back to Home without reloading. | Every visible string changes at once; no reload needed. | `R-1.2` C-18 | | |
| MQ-08.03 | Reload. | Still Hindi (saved on the account and on the device). | `R-1.2` | | |
| MQ-08.04 | Sign out and in again on a fresh profile as C11. | The app opens in Hindi (saved on the account). | `R-1.2` | | |

### B. English

Font: Figtree (Latin) with Anek for headings.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.05 | Set the language to English. Open Home (C-03). | Location label, search field, StoreMap stats (4 shops nearby, 4 open now), radius chips, category tiles, "Shops near you" and "Offers near you" all in the language. The "5% back", "Delivery" and "In-store only" tags translated and inside their pills. | `R-1.2` G3 C-03 | ▢ | |
| MQ-08.06 | Search "amul" (C-05). Open the filter row. | Suggestions and results translated around the product names. Chips (open now, delivery, Offers, category, sort) fit or scroll sideways without clipping. Amul Butter ₹58 with MRP ₹60 struck; stock tag "updated … ago" translated. | `R-1.2` `R-4.6` G3 C-05 | ▢ | |
| MQ-08.07 | Open Sri Lakshmi (C-07). | Header: open state with today's hours, "0.6 km", rating and count, cashback % and delivery terms (fee ₹30, free above ₹500, minimum ₹150) in the language. Toor dal shows the translated "Stock not confirmed recently". | `R-1.2` `R-4.6` G3 C-07 | ▢ | |
| MQ-08.08 | Add Amul Butter ×2 and Wheat atta ×1 (₹361). Open the cart and checkout (C-09, C-11). Turn credits on, 30. | "Use up to 30 credits on orders of ₹100 or more" translated with both numbers. Payment note "Pay at the shop: cash or UPI" translated. Totals: items ₹361, credits −₹30, total ₹331. Place order button stays 48 px high and the label is not cut. | `R-1.2` `R-7.3` G3 G7 C-09 C-11 | ▢ | |
| MQ-08.09 | Place the order (pickup). Read C-12 while it waits. | "Waiting for Sri Lakshmi to accept · m:ss left" translated with the shop name kept as written; the countdown digits keep their width (no jumping). | `R-1.2` `R-5.3` G3 C-12 | ▢ | |
| MQ-08.10 | In profile B accept the order. Read C-12 again, then cancel it with "Changed my mind" (translated). | PickupCode, hold countdown and "I'm on my way (+10 min)" translated. Cancel reasons are the four customer-cancel reasons, translated. After cancel the outcome is translated and the wallet history (C-15) shows the returned credits line in the language with the shop name and order number. | `R-1.2` `R-5.34` `R-7.13` CR-20 G8 C-12 C-15 | ▢ | |

### C. हिन्दी (Hindi)

Font: Anek Devanagari, Noto Sans Devanagari as the subset fallback.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.11 | Set the language to हिन्दी (Hindi). Open Home (C-03). | Location label, search field, StoreMap stats (4 shops nearby, 4 open now), radius chips, category tiles, "Shops near you" and "Offers near you" all in the language. The "5% back", "Delivery" and "In-store only" tags translated and inside their pills. | `R-1.2` G3 C-03 | ▢ | |
| MQ-08.12 | Search "amul" (C-05). Open the filter row. | Suggestions and results translated around the product names. Chips (open now, delivery, Offers, category, sort) fit or scroll sideways without clipping. Amul Butter ₹58 with MRP ₹60 struck; stock tag "updated … ago" translated. | `R-1.2` `R-4.6` G3 C-05 | ▢ | |
| MQ-08.13 | Open Sri Lakshmi (C-07). | Header: open state with today's hours, "0.6 km", rating and count, cashback % and delivery terms (fee ₹30, free above ₹500, minimum ₹150) in the language. Toor dal shows the translated "Stock not confirmed recently". | `R-1.2` `R-4.6` G3 C-07 | ▢ | |
| MQ-08.14 | Add Amul Butter ×2 and Wheat atta ×1 (₹361). Open the cart and checkout (C-09, C-11). Turn credits on, 30. | "Use up to 30 credits on orders of ₹100 or more" translated with both numbers. Payment note "Pay at the shop: cash or UPI" translated. Totals: items ₹361, credits −₹30, total ₹331. Place order button stays 48 px high and the label is not cut. | `R-1.2` `R-7.3` G3 G7 C-09 C-11 | ▢ | |
| MQ-08.15 | Place the order (pickup). Read C-12 while it waits. | "Waiting for Sri Lakshmi to accept · m:ss left" translated with the shop name kept as written; the countdown digits keep their width (no jumping). | `R-1.2` `R-5.3` G3 C-12 | ▢ | |
| MQ-08.16 | In profile B accept the order. Read C-12 again, then cancel it with "Changed my mind" (translated). | PickupCode, hold countdown and "I'm on my way (+10 min)" translated. Cancel reasons are the four customer-cancel reasons, translated. After cancel the outcome is translated and the wallet history (C-15) shows the returned credits line in the language with the shop name and order number. | `R-1.2` `R-5.34` `R-7.13` CR-20 G8 C-12 C-15 | ▢ | |

### D. ಕನ್ನಡ (Kannada)

Font: Anek Kannada, Noto Sans Kannada as the subset fallback.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.17 | Set the language to ಕನ್ನಡ (Kannada). Open Home (C-03). | Location label, search field, StoreMap stats (4 shops nearby, 4 open now), radius chips, category tiles, "Shops near you" and "Offers near you" all in the language. The "5% back", "Delivery" and "In-store only" tags translated and inside their pills. | `R-1.2` G3 C-03 | ▢ | |
| MQ-08.18 | Search "amul" (C-05). Open the filter row. | Suggestions and results translated around the product names. Chips (open now, delivery, Offers, category, sort) fit or scroll sideways without clipping. Amul Butter ₹58 with MRP ₹60 struck; stock tag "updated … ago" translated. | `R-1.2` `R-4.6` G3 C-05 | ▢ | |
| MQ-08.19 | Open Sri Lakshmi (C-07). | Header: open state with today's hours, "0.6 km", rating and count, cashback % and delivery terms (fee ₹30, free above ₹500, minimum ₹150) in the language. Toor dal shows the translated "Stock not confirmed recently". | `R-1.2` `R-4.6` G3 C-07 | ▢ | |
| MQ-08.20 | Add Amul Butter ×2 and Wheat atta ×1 (₹361). Open the cart and checkout (C-09, C-11). Turn credits on, 30. | "Use up to 30 credits on orders of ₹100 or more" translated with both numbers. Payment note "Pay at the shop: cash or UPI" translated. Totals: items ₹361, credits −₹30, total ₹331. Place order button stays 48 px high and the label is not cut. | `R-1.2` `R-7.3` G3 G7 C-09 C-11 | ▢ | |
| MQ-08.21 | Place the order (pickup). Read C-12 while it waits. | "Waiting for Sri Lakshmi to accept · m:ss left" translated with the shop name kept as written; the countdown digits keep their width (no jumping). | `R-1.2` `R-5.3` G3 C-12 | ▢ | |
| MQ-08.22 | In profile B accept the order. Read C-12 again, then cancel it with "Changed my mind" (translated). | PickupCode, hold countdown and "I'm on my way (+10 min)" translated. Cancel reasons are the four customer-cancel reasons, translated. After cancel the outcome is translated and the wallet history (C-15) shows the returned credits line in the language with the shop name and order number. | `R-1.2` `R-5.34` `R-7.13` CR-20 G8 C-12 C-15 | ▢ | |

### E. తెలుగు (Telugu)

Font: Anek Telugu, Noto Sans Telugu as the subset fallback.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.23 | Set the language to తెలుగు (Telugu). Open Home (C-03). | Location label, search field, StoreMap stats (4 shops nearby, 4 open now), radius chips, category tiles, "Shops near you" and "Offers near you" all in the language. The "5% back", "Delivery" and "In-store only" tags translated and inside their pills. | `R-1.2` G3 C-03 | ▢ | |
| MQ-08.24 | Search "amul" (C-05). Open the filter row. | Suggestions and results translated around the product names. Chips (open now, delivery, Offers, category, sort) fit or scroll sideways without clipping. Amul Butter ₹58 with MRP ₹60 struck; stock tag "updated … ago" translated. | `R-1.2` `R-4.6` G3 C-05 | ▢ | |
| MQ-08.25 | Open Sri Lakshmi (C-07). | Header: open state with today's hours, "0.6 km", rating and count, cashback % and delivery terms (fee ₹30, free above ₹500, minimum ₹150) in the language. Toor dal shows the translated "Stock not confirmed recently". | `R-1.2` `R-4.6` G3 C-07 | ▢ | |
| MQ-08.26 | Add Amul Butter ×2 and Wheat atta ×1 (₹361). Open the cart and checkout (C-09, C-11). Turn credits on, 30. | "Use up to 30 credits on orders of ₹100 or more" translated with both numbers. Payment note "Pay at the shop: cash or UPI" translated. Totals: items ₹361, credits −₹30, total ₹331. Place order button stays 48 px high and the label is not cut. | `R-1.2` `R-7.3` G3 G7 C-09 C-11 | ▢ | |
| MQ-08.27 | Place the order (pickup). Read C-12 while it waits. | "Waiting for Sri Lakshmi to accept · m:ss left" translated with the shop name kept as written; the countdown digits keep their width (no jumping). | `R-1.2` `R-5.3` G3 C-12 | ▢ | |
| MQ-08.28 | In profile B accept the order. Read C-12 again, then cancel it with "Changed my mind" (translated). | PickupCode, hold countdown and "I'm on my way (+10 min)" translated. Cancel reasons are the four customer-cancel reasons, translated. After cancel the outcome is translated and the wallet history (C-15) shows the returned credits line in the language with the shop name and order number. | `R-1.2` `R-5.34` `R-7.13` CR-20 G8 C-12 C-15 | ▢ | |

### F. தமிழ் (Tamil)

Font: Anek Tamil, Noto Sans Tamil as the subset fallback.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.29 | Set the language to தமிழ் (Tamil). Open Home (C-03). | Location label, search field, StoreMap stats (4 shops nearby, 4 open now), radius chips, category tiles, "Shops near you" and "Offers near you" all in the language. The "5% back", "Delivery" and "In-store only" tags translated and inside their pills. | `R-1.2` G3 C-03 | ▢ | |
| MQ-08.30 | Search "amul" (C-05). Open the filter row. | Suggestions and results translated around the product names. Chips (open now, delivery, Offers, category, sort) fit or scroll sideways without clipping. Amul Butter ₹58 with MRP ₹60 struck; stock tag "updated … ago" translated. | `R-1.2` `R-4.6` G3 C-05 | ▢ | |
| MQ-08.31 | Open Sri Lakshmi (C-07). | Header: open state with today's hours, "0.6 km", rating and count, cashback % and delivery terms (fee ₹30, free above ₹500, minimum ₹150) in the language. Toor dal shows the translated "Stock not confirmed recently". | `R-1.2` `R-4.6` G3 C-07 | ▢ | |
| MQ-08.32 | Add Amul Butter ×2 and Wheat atta ×1 (₹361). Open the cart and checkout (C-09, C-11). Turn credits on, 30. | "Use up to 30 credits on orders of ₹100 or more" translated with both numbers. Payment note "Pay at the shop: cash or UPI" translated. Totals: items ₹361, credits −₹30, total ₹331. Place order button stays 48 px high and the label is not cut. | `R-1.2` `R-7.3` G3 G7 C-09 C-11 | ▢ | |
| MQ-08.33 | Place the order (pickup). Read C-12 while it waits. | "Waiting for Sri Lakshmi to accept · m:ss left" translated with the shop name kept as written; the countdown digits keep their width (no jumping). | `R-1.2` `R-5.3` G3 C-12 | ▢ | |
| MQ-08.34 | In profile B accept the order. Read C-12 again, then cancel it with "Changed my mind" (translated). | PickupCode, hold countdown and "I'm on my way (+10 min)" translated. Cancel reasons are the four customer-cancel reasons, translated. After cancel the outcome is translated and the wallet history (C-15) shows the returned credits line in the language with the shop name and order number. | `R-1.2` `R-5.34` `R-7.13` CR-20 G8 C-12 C-15 | ▢ | |

### G. Business app and pseudo-locale

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-08.35 | In profile B (K22) set the business app language to ಕನ್ನಡ. Open Orders (B-04) and Sell (B-05). | Tabs, order cards, the total bar and Charge in Kannada; no clipping in the 7 order tabs at 360 px (they scroll sideways). | `R-1.2` G3 B-04 B-05 | ▢ | |
| MQ-08.36 | Open https://app.todayz.localhost/?locale=pseudo on Home, Search, Shop, Checkout and Tracking. | Every string is accented and about 40% longer (en-XA). Nothing clips or overlaps; any string that is not accented is a hard-coded string: record its screen. | G3 D-87 | ▢ | |
| MQ-08.37 | Remove `?locale=pseudo` and reload. | Back to the saved language. | `R-1.2` | | |

## Cleanup

1. Set C11's language back to English.
2. `clock reset`; `make reset`.
