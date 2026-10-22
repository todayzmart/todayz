# I18N Languages in detail

MQ-08 walks one order in each language. This file checks what MQ-08 does not: the business app and admin in each language, numbers, dates and ₹, long strings at 360 px, the pseudo-locale on the business app, server text (problem messages, notifications, reasons) and search across scripts.

- **Covers:** SC-I18N; CR-20; R-1.2; D-87.
- **Screens:** C-01, C-03, C-05, C-07, C-09, C-11, C-12, C-15, C-18, C-24; B-03, B-04, B-05, B-10, B-14; A-07.
- **Devices:** P360 (customer), D1280 and P360 (business).
- **Time:** about 90 minutes.

## Preconditions

1. `make reset`. `<seed:pseudo>` reachable.
2. Five languages: English, हिन्दी, ಕನ್ನಡ, తెలుగు, தமிழ். Keep a language table in the run notes.

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| I18N-M.01 | Open the customer app in a fresh profile with the browser language set to Kannada. | C-01 pre-selects ಕನ್ನಡ (browser language) and lists all five in their own script. Skip → Kannada stays. | `R-1.2` C-01 | ▢ | |
| I18N-M.02 | As C11 (Kannada) open C-15 Wallet and C-09 with credits on. | Credits and ₹ amounts use the same digits as the rest of the app (Latin digits unless the build chose native digits; record, Q-34). ₹ is before the number with no space issue (₹1,240.50 or the build's format). Indian grouping (₹1,00,000) for large numbers. | CR-20 Q-34 C-15 | ▢ | |
| I18N-M.03 | Read a date and a time on C-12 and C-15 (lot expiry) in each of the five languages. | Dates in the language's month names, IST times, 12-hour with the localised am/pm or 24-hour; record. No UTC times. | CR-20 | | |
| I18N-M.04 | In Tamil, open C-03, C-05, C-07, C-09 and C-11 at P360. | No text cut off or overlapping; buttons wrap or shrink, never clip. Record any screen that scrolls sideways. | SC-I18N | ▢ | |
| I18N-M.05 | Open the business app with `?locale=pseudo` on B-03, B-04, B-05, B-07, B-14 at P360 and D1280. | Every visible string is pseudo-localised (accented, about 40% longer, bracketed). Any plain English string is a missed key: list it. Layout holds. | D-87 SC-I18N | ▢ | |
| I18N-M.06 | Open the admin console with `?locale=pseudo` on A-07 and A-17. | Record whether admin is translated (06 does not require admin in five languages); at least no crash. | D-87 | | |
| I18N-M.07 | K22 sets the business app to తెలుగు. Accept, reject (pick a reason), partial accept and hand over one order each. | Every reason, state chip, toast and error is in Telugu. The reject reason the customer sees is in the customer's own language, not the shop's. | BK-07 SC-I18N | ▢ | |
| I18N-M.08 | In Hindi, trigger `store_closed` (order at 22:01), `insufficient_stock`, `below_credit_minimum` and `code_locked`. | Each error shows a Hindi message and "Try again" (06 G1). No English `detail` text. | PL-02 SC-I18N | | |
| I18N-M.09 | Search in Kannada script "ಹಾಲು" (milk) and in transliteration "haalu", and "dahi" in English. | Milk items found for both Kannada inputs; "dahi" finds curd (synonym). Record the result counts. | SC-SR `R-11.3` | ▢ | |
| I18N-M.10 | Open a POS receipt (C-24) after setting the shop language to Hindi and the customer to English. | Record which language the receipt uses. Amounts and ₹ format as in M.02. | C-24 `R-12.10` | | |
| I18N-M.11 | In each language open C-18 → Delete account and C-21 limit banner (C13). | Both translated; the date in the C-21 banner is localised. | C-18 C-21 | | |
| I18N-M.12 | Switch language mid-checkout on C-11 (E2E-15 manual side): Hindi → English → Tamil without reloading. | The cart, quote, credits and selected address stay. Only the text changes. | E2E-15…20 | ▢ | |

## Cleanup

1. Set every profile back to English. `make reset`.
