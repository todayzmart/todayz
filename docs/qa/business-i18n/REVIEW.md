# Business app languages (Hindi, Kannada, Telugu, Tamil): review

Claude machine-wrote the translations in `shared/i18n/messages/business/*.{hi,kn,te,ta}.json`. They cover all 1374 business keys in each language (13 areas). **They were released on the founder's instruction (25 Oct) without a native-speaker review.** Nobody fluent in Hindi, Kannada, Telugu or Tamil has read them yet. Each language section below lists the strings I was unsure of.

**Reviewer: none yet (native speaker needed). Approver: founder.**

## Approval record

| Date | Approver | What | Note |
|---|---|---|---|
| 2026-10-25 | founder (Rahul) | Business app Hindi, Kannada, Telugu and Tamil: all 1374 keys each (`review: reviewed`) | Released on the founder's instruction (25 Oct) without a native-speaker review. Argument checks pass (`lint:messages`: business 0 errors, 1374/1374 reviewed per language). Each language's uncertain strings are listed below and still need a native speaker's read. |

`translations.mjs approve` only sets `review: reviewed` in the catalogue. This table is the record of who approved and when.

The 23 keys W2-16 added to `pos.en.json` (the "Other" payment method and the store sales sheet) were translated after merging `main` into this branch. They went through the same approval.

What this changes on release:

- Every business locale is reviewed, so production builds of the business app ship hi, kn, te and ta with no key in English.
- Settings (B-14) gets a **Language** section for every role (owner, manager, cashier). See "Wiring" below.

## Wiring

- `business/src/app/i18n.ts`: `loadMessages(locale)` imports a single compiled catalogue (one lazy chunk per language), and `initialLocale()` reads `detectLocale()`.
- `business/src/app/locale.ts`: `LocaleContext` / `useLocale()` (`locale`, `choose`).
- `business/src/app/App.tsx`: holds `{ locale, messages }`. `choose()` loads the catalogue, then saves the choice (`saveLocale`, localStorage `todayz.lang`), sets `<html lang>` (`applyLocale`) and re-renders. Formatting uses `FORMAT_LOCALE` for en and en-XA, otherwise `intlTag(locale)`.
- `business/src/main.tsx`: loads the detected language before the first render.
- `business/src/settings/LanguageSection.tsx`: the shared `LanguagePicker` (native names, `aria-pressed`). It renders above the role-gated parts of `SettingsPage`, so a cashier can switch too.
- `settings.en.json`: 8 new `settings.language.*` keys.

The choice is kept on this device only. Saving it on the server (`preferredLang`, so it follows the person to another device and drives SMS/WhatsApp language) is a follow-up; it is not done here.

## Evidence

- `business/src/settings/language.test.tsx` (4 tests, pass):
  - Every English key has a `reviewed` translation in hi, kn, te and ta whose `enHash` matches the current English. This is read from the source files, because compiled catalogues fill a missing or stale translation with English. Setting one Tamil key back to `machine` makes it fail (`ta pos.pay.other`), so a new English key without translations fails `make check`.
  - Every compiled catalogue (en, hi, kn, te, ta, en-XA) has the English key set.
  - The hi, kn, te and ta catalogues are translated, not English copies.
  - A cashier opens Settings in English and picks हिन्दी. The page re-renders in Hindi (the section heading "भाषा" and the role notice), the English heading is gone, the button is pressed, `<html lang="hi">` is set and `todayz.lang` is `hi`.
- `pnpm --filter @todayz/i18n lint:messages`: business 0 errors, 307 warnings (all "not referenced by a literal id", meaning the id is built at run time; these come from the English catalogue). hi/kn/te/ta 1374/1374 reviewed.
- `@todayz/i18n`: lint, typecheck, test pass (4 files, 198 tests).
- `@todayz/business`: lint, lint:css, typecheck pass; tests pass (42 files, 379 tests); build passes (precache check and dist check pass); size: initial JS 162.24 kB gzip of 250 kB, initial CSS 10.35 kB of 20 kB.

## How to review

1. In `make dev`, open https://business.todayz.localhost → Settings → Language and pick the language.
2. Read each screen against the English (`*.en.json`, same key). Use the checklist in `shared/i18n/docs/translation-workflow.md`: the meaning matches, plurals read right, nothing is cut off, and each term is the same everywhere.
3. To change a string, edit its `message` in `shared/i18n/messages/business/<area>.<lc>.json`, run `make gen`, then `pnpm --filter @todayz/i18n lint:messages`.
4. Record the native review in the approval table above.

## Terms used throughout

Each term is used the same way on every screen. If you change one, change it everywhere.

| English | Hindi | Kannada | Telugu | Tamil |
|---|---|---|---|---|
| order | ऑर्डर | ಆರ್ಡರ್ | ఆర్డర్ | ஆர்டர் |
| stock | स्टॉक | ಸ್ಟಾಕ್ | స్టాక్ | ஸ்டாக் |
| item | आइटम | ಐಟಂ | ఐటమ్ | பொருள் |
| customer | ग्राहक | ಗ್ರಾಹಕರು | కస్టమర్ | வாடிக்கையாளர் |
| product | प्रोडक्ट | ಉತ್ಪನ್ನ | ప్రోడక్ట్ | தயாரிப்பு |
| sale / sales | बिक्री | ಮಾರಾಟ / ಮಾರಾಟಗಳು | అమ్మకం / అమ్మకాలు | விற்பனை / விற்பனைகள் |
| store, shop | दुकान | ಅಂಗಡಿ | దుకాణం | கடை |
| bill | बिल | ಬಿಲ್ | బిల్లు | பில் |
| credits | क्रेडिट | ಕ್ರೆಡಿಟ್ | క్రెడిట్‌లు | கிரெடிட் |
| offer | ऑफ़र | ಆಫರ್ | ఆఫర్ | ஆஃபர் |
| pickup code, code | कोड | ಕೋಡ್ | కోడ్ | குறியீடு |
| link (a customer) | लिंक | ಲಿಂಕ್ | లింక్ | இணை / இணைப்பு |
| sync | सिंक | ಸಿಂಕ್ | సింక్ | ஒத்திசைவு |
| import | इंपोर्ट | ಇಂಪೋರ್ಟ್ | ఇంపోర్ట్ | இறக்குமதி |
| expiry | एक्सपायरी | ಅವಧಿ | గడువు | காலாவதி |
| batch | बैच | ಬ್ಯಾಚ್ | బ్యాచ్ | பேட்ச் |
| offline | ऑफ़लाइन | ಆಫ್‌ಲೈನ್ | ఆఫ్‌లైన్ | ஆஃப்லைன் |

These stay as written in every language: TodayZ, POS, UPI, MRP, GST, KYC, PIN, OTP, QR, CSV/XLSX, MB, product and shop names, and every `{placeholder}`. Money is ₹ and comes from the placeholders.

Choices that apply to every language:

- `stock.form.qtyInvalid` and `stock.form.countInvalid` write the limit as **10,00,000** (Indian grouping), where the English has 1,000,000. The value is the same.
- Units stay Latin: `{seconds} s` (`auth.code.resendIn`), `{h} h {m} min`, "72 h" (`orders.return.*.unconfirmed`).
- The Overview chart axis labels `{h}a` (`overview.chart.hourAm`) and `₹{k}k` (`overview.chart.axisK`) are kept exactly as in English, because they are compact axis ticks.
- Import problems (`stock.import.code.*`) render after a field name as "{field}: {problem}", so each one is a short predicate ("is missing" → नहीं है / ಇಲ್ಲ / లేదు / இல்லை). The grammar in all four languages is approximate.
- Case endings after a placeholder (`{name} से`, `{name} ಅವರಿಗೆ`, `{name} కు`, `{name} உடன்`) are attached with a space, because the value is not known in advance.

## Hindi: strings to check

| Key | English | Hindi | Why |
|---|---|---|---|
| `pos.banner.waiting.title` (and other plurals) | {count, plural, one {# sale waiting…} other {# sales waiting…}} | `one` and `other` are identical (# बिक्री सिंक के इंतज़ार में) | Hindi does not change "बिक्री" here; both forms are required by ICU. Check that every plural reads right with 1. |
| `credits.line.adjustment` | Adjustment | सुधार | सुधार means "correction". A credit adjustment can go up or down; समायोजन is the formal option. |
| `stock.form.writeOff` | {quantity} will be written off. | {quantity} बट्टे खाते में जाएँगे। | Accounting phrase; shop staff may say "स्टॉक से हटेंगे" instead (the other three languages say "removed from stock"). |
| `orders.card.acceptPart` | Accept part | कुछ हिस्सा स्वीकार करें | Button text; check it is short enough and clear on a phone. |
| `orders.tab.asks` | Asks | माँगें | Tab for customer requests (B-20). It may read as "demands". |
| `orders.action.partSent` etc. | confirm | पक्का करना | Used for "confirm" in order flows. Check it is natural, versus कन्फ़र्म करें. |
| `orders.reason.too_slow` | Customer found the reply too slow | ग्राहक को जवाब बहुत धीमा लगा | A rejection reason; the phrasing is literal. |
| `offers.status.scheduled` | Scheduled | शेड्यूल्ड | Transliterated; a native alternative is तय समय पर. |
| `offers.status.live` | Live | लाइव | Transliterated. |
| `pos.title`, `shell.nav.sell` | Sell | बेचें | A verb ("sell"), used as the tab name. |
| `offers.banner.tint.*` | Green, Amber, Rose, Sky, Lilac | approximate colour names (Lilac → बैंगनी) | The banner tints are pastel; the names are close, not exact. |
| `overview.setup.next.openingHours` | …“Open now” searches. | “अभी खुला है” | Must match the filter wording in the customer app. |
| `pos.storeSales.offlineSale` | Rung up offline | ऑफ़लाइन बिकी | Short tag; check the gender and wording. |

## Kannada: strings to check

| Key | English | Kannada | Why |
|---|---|---|---|
| all `customer` strings, e.g. `pos.cart.linkCustomer` | Link customer | ಗ್ರಾಹಕರನ್ನು ಲಿಂಕ್ ಮಾಡಿ | ಗ್ರಾಹಕರು (plural/honorific) is used for one customer as a polite form. Check it is natural throughout. |
| `pos.title`, `shell.nav.sell` | Sell | ಮಾರಾಟ | A noun ("sale"), not a verb, used as the tab name. |
| `orders.card.acceptPart` | Accept part | ಭಾಗಶಃ ಸ್ವೀಕರಿಸಿ | Check it means "accept part of the order". |
| `orders.tab.asks` | Asks | ಕೇಳಿಕೆಗಳು | Tab for customer requests; the word choice is uncertain. |
| `credits.line.payout` | Payout | ಪೇಔಟ್ | Transliterated. |
| `credits.line.adjustment` | Adjustment | ಹೊಂದಾಣಿಕೆ | Check it reads as a credit adjustment. |
| `stock.form.writeOff` | {quantity} will be written off. | {quantity} ಅನ್ನು ಸ್ಟಾಕ್‌ನಿಂದ ತೆಗೆಯಲಾಗುತ್ತದೆ. | "Removed from stock" rather than an accounting term. |
| `offers.status.live` | Live | ಲೈವ್ | Transliterated. |
| `orders.reason.too_slow` | Customer found the reply too slow | ಗ್ರಾಹಕರಿಗೆ ಉತ್ತರ ತುಂಬಾ ತಡವಾಯಿತು | It says "the reply was too late"; check the meaning holds. |
| `stock.form.expiry` and other expiry strings | Expiry date | ಅವಧಿ ದಿನಾಂಕ | ಅವಧಿ is "period/term"; ಮುಕ್ತಾಯ ದಿನಾಂಕ may be clearer for a date. |
| `offers.banner.tint.*` | Lilac etc. | approximate (Lilac → ನೇರಳೆ) | The colour names are close, not exact. |
| `pos.storeSales.link.linkedTo` | Linked to {name} | {name} ಅವರಿಗೆ ಲಿಂಕ್ ಆಗಿದೆ | Case ending after a placeholder. |

## Telugu: strings to check

| Key | English | Telugu | Why |
|---|---|---|---|
| `pos.title`, `shell.nav.sell` | Sell | అమ్మండి | A verb ("sell"), used as the tab name. |
| `orders.card.acceptPart` | Accept part | కొంత భాగం అంగీకరించండి | Button text; check its length and clarity. |
| `orders.tab.asks` | Asks | అభ్యర్థనలు | Tab for customer requests. |
| `credits.line.payout` | Payout | పేఅవుట్ | Transliterated. |
| `credits.line.adjustment` | Adjustment | సవరణ | Check it reads as a credit adjustment. |
| `stock.form.writeOff` | {quantity} will be written off. | {quantity} స్టాక్ నుంచి తీసివేయబడతాయి. | "Removed from stock". |
| `offers.status.live` | Live | లైవ్ | Transliterated. |
| `orders.reason.too_slow` | Customer found the reply too slow | కస్టమర్‌కు జవాబు చాలా ఆలస్యంగా అనిపించింది | Literal phrasing. |
| `offers.banner.tint.lilac` | Lilac | లేత ఊదా | "Light purple": close, not exact. |
| `pos.storeSales.link.linkedTo` | Linked to {name} | {name} కు లింక్ అయింది | కు vs కి depends on the name; a fixed suffix is used. |
| `pos.storeSales.failure.unverified` | The code could not be checked. | కోడ్‌ను తనిఖీ చేయలేకపోయాం. | First person plural ("we could not"); check the tone. |

## Tamil: strings to check

| Key | English | Tamil | Why |
|---|---|---|---|
| `pos.title`, `shell.nav.sell` | Sell | விற்பனை | A noun ("sale"), used as the tab name. |
| `credits.line.payout` | Payout | பணம் வழங்கல் | Native coinage ("money disbursal"); a transliteration (பேஅவுட்) may be what shops expect. |
| `credits.line.adjustment` | Adjustment | சரிசெய்தல் | Check it reads as a credit adjustment. |
| `stock.batch.cost`, `stock.form.cost` | Cost | அடக்கவிலை | Trade term for cost price; check that shop staff know it. |
| `stock.form.writeOff` | {quantity} will be written off. | {quantity} ஸ்டாக்கிலிருந்து கழிக்கப்படும். | "Deducted from stock". |
| `orders.card.acceptPart` | Accept part | ஒரு பகுதியை ஏற்கவும் | Check it means "accept part of the order". |
| `orders.tab.asks` | Asks | கோரிக்கைகள் | Tab for customer requests. |
| `offers.status.live` | Live | நேரலையில் | Native ("on air"), unlike the other three, which transliterate. Check that it fits an offer. |
| `pos.cart.roundOff` | Round off | முழுமையாக்கல் | Native coinage; ரவுண்ட் ஆஃப் may be what cashiers say. |
| `pos.sync.title` and every sync string | Sync | ஒத்திசைவு | Native term; the other three transliterate "sync". |
| `offers.kind.bxgy` | Buy and get free | வாங்கினால் இலவசம் | "Free if you buy": short, but check it. |
| `offers.banner.tint.lilac` | Lilac | இளஞ்சிவப்பு ஊதா | "Pinkish purple": approximate. |
| `pos.storeSales.link.linkedTo` | Linked to {name} | {name} உடன் இணைக்கப்பட்டது | Case ending after a placeholder. |
