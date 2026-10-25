# W4-05 Customer app languages (Hindi, Kannada, Telugu, Tamil): review

The translations in `shared/i18n/messages/customer/*.{hi,kn,te,ta}.json` were machine-written (by Claude) for all 490 customer keys in each language. The founder reviewed the Hindi. Kannada, Telugu and Tamil were released on the founder's instruction without a native-speaker review (see the approval record).

**Reviewer: founder.**

## Approval record

| Date | Approver | What | Note |
|---|---|---|---|
| 2026-10-25 | founder (Rahul) | Customer app Hindi: all 490 keys (`review: reviewed`) | The founder read the Hindi and approved it, including the strings listed below as they are. |
| 2026-10-25 | founder (Rahul) | Customer app Kannada, Telugu and Tamil: all 490 keys each (`review: reviewed`) | Released on the founder's instruction (25 Oct) without a native-speaker review. Argument checks pass (`lint:messages`: customer 0 errors). The strings I was unsure of are listed per language below; they still need a native speaker's read. |
| 2026-10-25 | founder (Rahul) | Site (`shared/i18n/messages/site/{common,home,retailers}.{hi,kn,te,ta}.json`): the 261 existing machine drafts per language | Approved on the founder's instruction. Argument checks pass (`lint:messages`: site 0 errors). Kannada, Telugu and Tamil were not reviewed by a native speaker. |
| 2026-10-25 | founder (Rahul), relayed by the W3-08 lead | W3-08 C-19 Help: the 81 customer `help.*` keys in hi, kn, te and ta | Approved under the founder's standing instruction ("go live with hindi", then "and all other languages too"), relayed by the lead on 25 Oct. Machine-written; no native-speaker review, Hindi included. Argument checks pass (`lint:messages`: customer 0 errors). Status words match the business app ("Replied" जवाब मिला, "Solved" हल हुआ), and "support" is transliterated (सपोर्ट). The example subject in `help.new.subject.help` (“Atta missing from my order”) is translated inside the quotes. |
| 2026-10-26 | founder (Rahul), relayed by the W3-16 lead | W3-16 C-25 Ask, C-26 replies and C-08 share: the 76 customer keys (63 `asks.*`, 13 `share.*`) in hi, kn, te and ta | Approved under the founder's standing instruction (25 Oct), relayed by the lead in the W3-16 review. Machine-written; no native-speaker review, Hindi included. Placeholders are unchanged and argument checks pass (`lint:messages`: customer 0 errors). Terms: an ask is माँग / ಕೇಳಿಕೆ / అభ్యర్థన / கோரிக்கை; a shop is दुकान / ಅಂಗಡಿ / దుకాణం / கடை. Strings to check: `asks.reply.held` (Hindi "दाम {time} के लिए पक्का"; kn, te and ta say the price is still certain for {time}), `asks.reply.replied` (starts with {ago}), `asks.progress.best` ("best" is rendered as "lowest price"), `asks.sheet.privacy`, `asks.none` and `share.more`. |

`translations.mjs approve` only sets `review: reviewed` in the catalogue; this table is the record of who approved and when.

What this changes on release:

- Every customer locale is reviewed, so production builds of the customer app ship hi, kn, te and ta.
- Every site locale is reviewed in the three namespaces the site checks (common, home, retailers), so production builds of the site ship en, hi, kn, te and ta (`siteLocales('production')`). The site's 88 other keys per language (info, legal, shell) have no translation yet and show in English. `site/src/i18n/i18n.test.ts` and `site/src/lib/verify-dist.test.ts` now expect this.
- `make dev` and `make e2e` still compile machine drafts (`TODAYZ_I18N_DRAFTS=1`); production builds never do. With everything reviewed, the flag only matters for future drafts.

## Evidence

E2E-15 (`e2e/journeys/specs/08-e2e-15-language.spec.ts`) passes 3 of 3 on a fresh `make reset`, under `scripts/no-egress.sh`: the mid-journey switch to Hindi, the Hindi screens, and a light check that Home reads in Telugu. Screenshots at 360 px wide, full page (the bottom navigation is fixed, so it shows part-way down a full-page capture):

| Screen | File |
|---|---|
| Home (C-02), Hindi | [hi-home-360.png](hi-home-360.png) |
| Cart (C-09), Hindi | [hi-cart-360.png](hi-cart-360.png) |
| Checkout (C-11), Hindi | [hi-checkout-360.png](hi-checkout-360.png) |
| Home (C-02), Telugu | [te-home-360.png](te-home-360.png) |

## How to review

1. Open the customer app in the language: pick हिन्दी, ಕನ್ನಡ, తెలుగు or தமிழ் on the language screen (`/language`).
2. Read each screen against the English (`*.en.json`, same key). The checklist is in `shared/i18n/docs/translation-workflow.md`: the meaning matches, plurals read right, nothing is cut off, and each term is the same everywhere.
3. To change a string, edit its `message` in `shared/i18n/messages/customer/<area>.<lc>.json` (`lc` is hi, kn, te or ta).
4. Approve the keys you have read: `node shared/i18n/scripts/translations.mjs approve customer <area> <lc> <key...>`. Keys you have not approved stay marked as drafts (`"review": "machine"`).
5. Run `make gen`, then `pnpm --filter @todayz/i18n lint:messages`.

## Hindi

### Terms used throughout

Each of these is used the same way on every screen. If you change one, change it everywhere.

| English | Hindi used |
|---|---|
| order | ऑर्डर |
| cart | कार्ट |
| checkout | चेकआउट |
| pickup | पिकअप |
| delivery | डिलीवरी |
| stock | स्टॉक |
| credits | क्रेडिट |
| shop | दुकान |
| item (in prose) / item (in counts) | सामान / आइटम |
| basket | बास्केट (feminine) |
| offers | ऑफ़र |
| account | खाता |
| confirmed (stock) | पक्का किया |
| accept (order) | मंज़ूर करना |
| cancel | कैंसल |

These stay as written: TodayZ, OTP, UPI, PIN, SMS, km. Digits stay Latin (0-9), and prices come in already formatted as ₹.

### Strings I was unsure of

| Key | English | My Hindi | Why unsure |
|---|---|---|---|
| `cart.checkout`, `checkout.title` | Checkout | चेकआउट | A loanword. "ऑर्डर पक्का करें" may be clearer for a first-time user, but it is long for a button. |
| `cart.count`, `basket.list.count`, `orders.row.meta` | `{count, plural, one {# item} other {# items}}` | `# आइटम` in both forms | Hindi does not change the word for the plural. Would सामान read better than आइटम in a count? |
| `orders.accepted.note` | {shop} accepted your order and is keeping it for you. | {shop} ने आपका ऑर्डर मंज़ूर कर लिया है और आपके लिए रख रही है। | The verb is feminine to agree with दुकान, but `{shop}` is a shop name (for example "Sri Lakshmi Stores"). Masculine or a neutral form may read better. The same applies to `orders.outcome.rejected` (नहीं ले सकी) and `orders.partial.body` (नहीं दे सकी). |
| `basket.full`, `basket.conflict`, `basket.list.updated`, `basket.deleted` | (various) | भर गई, बदली गई, डिलीट हो गई | Basket is treated as feminine. Check that this sounds natural. |
| `product.confirmed`, `product.confirmedNow` | Confirmed {time} / Confirmed just now | {time} पक्का किया / अभी-अभी पक्का किया | `{time}` arrives as "12 मिनट पहले". Is "पक्का किया" the right word for "the shop confirmed the stock"? |
| `search.noneConfirmed.title` | None of the shops has confirmed yet. | अभी तक किसी दुकान ने पक्का नहीं किया है। | Same "पक्का" question. It needs the D-190 sense: no shop has confirmed the stock lately. |
| `store.inStoreOnly`, `orders.tabs.store` | In-store only / In store | सिर्फ़ दुकान पर / दुकान पर | "सिर्फ़ दुकान पर" might be read as "only at the shop" in the sense of location, not "no online orders". |
| `orders.status.placed` | Waiting for shop | दुकान के जवाब का इंतज़ार | Longer than the English and shown in a status pill. Check it fits at 360 px. |
| `orders.status.collected`, `orders.step.collected` | Collected | ले लिया | Terse. "आपने ले लिया" or "मिल गया" may read better. |
| `orders.status.rejected` | Declined | मना कर दिया | Could sound blunt. Alternative: "दुकान ने नहीं लिया". |
| `orders.limit.body` | You missed several pickups, so until {date} you can have {max, plural, one {one open order} other {# open orders}} at a time. | …{date} तक {max, plural, one {आपका एक ही ऑर्डर एक बार में खुला रह सकता है} other {आपके # ऑर्डर एक बार में खुले रह सकते हैं}}। | I moved the verb inside the plural so it agrees in number. Check that it reads well and is not too long. |
| `checkout.problem.ordering_suspended.until` | …until {date} you can have only one open order… | …{date} तक आपका एक ही ऑर्डर खुला रह सकता है… | "खुला ऑर्डर" (open order) may not be a phrase people use. |
| `cart.credits.blocked.below_credit_minimum` | Credits can be used when your items come to the shop's minimum for credits. Add more to use them. | क्रेडिट तभी इस्तेमाल होंगे जब आपका सामान दुकान की क्रेडिट वाली न्यूनतम रकम तक पहुँचे। … | "न्यूनतम रकम" is formal. Is there a simpler way to say "minimum amount"? |
| `cart.credits.using` | …Credits never pay for delivery. | …क्रेडिट से डिलीवरी का पैसा नहीं दिया जा सकता। | Check that the rule is still clear. |
| `basket.preview` | Totals are a preview. Checkout shows the final price. | ये रकम सिर्फ़ अंदाज़ा है। आख़िरी दाम चेकआउट पर दिखेगा। | "अंदाज़ा" (estimate) may make the price sound unreliable. |
| `basket.ask.example1`–`example4` | Aashirvaad atta 5 kg, Amul milk 500 ml, Dolo 650, Tata salt 1 kg | (kept in English) | These are examples of what to type. Shops list item names in English, so a Hindi example would not find anything. Should they be in Devanagari anyway? |
| `basket.name.placeholder` | Weekly groceries | हफ़्ते का राशन | "राशन" can suggest the ration shop (PDS). "हफ़्ते का सामान" is an option. |
| `auth.code.resendIn` | Resend in {seconds} s | {seconds} s में फिर से भेजें | Kept "s". "{seconds} सेकंड में" may be clearer. |
| `home.radius.km`, `home.empty.widen`, `search.noneConfirmed.wider` | {km} km | {km} km | Kept "km" in Latin. "किमी" is also common. |
| `location.current` | Shopping near {place} | {place} के पास खरीदारी | Short chip at the top of Home. It may read as a fragment. |
| `home.map.live` | Live near you | आपके पास, अभी | A small badge; the "live" sense is approximate. |
| `search.summary` | Checked {checked} shops nearby · {have} have it · from {min} | पास की {checked} दुकानें देखीं · {have} में है · {min} से | "{have} में है" for "{have} have it" is compact. Check that it is clear. |
| `search.sort.relevance` | Best match | सबसे सही | Could be "सबसे मिलता-जुलता". |
| `shell.skipToContent` | Skip to content | सीधे मुख्य हिस्से पर जाएँ | Screen-reader link. The wording is a guess. |
| `product.unit.pc` | {size} pc | {size} पीस | "पीस" is the spoken form. Check that it fits on the price line. |
| `app.offline.body` | Saved pages still open. We'll catch up when you're back online. | …ऑनलाइन होते ही सब अपडेट हो जाएगा। | "We'll catch up" is translated loosely as "everything will update". |
| `lang.name.en` | English | अंग्रेज़ी | This is the name shown under the native "English" on the language card, so both appear on the card. |

## Kannada, Telugu and Tamil

The same rules as the Hindi: simple everyday words, English retail words kept where people say them, Latin digits, ₹ from the formatter, placeholders and plural branches exactly as in English. TodayZ, OTP, UPI, PIN, SMS and km stay as written. A case ending after a placeholder is written as a separate word (`{shop} ನಲ್ಲಿ`, `{shop} లో`, `{shop} இல்`), because the shop name arrives in English.

### Terms used throughout

| English | Kannada | Telugu | Tamil |
|---|---|---|---|
| order | ಆರ್ಡರ್ | ఆర్డర్ | ஆர்டர் |
| cart | ಕಾರ್ಟ್ | కార్ట్ | கார்ட் |
| checkout | ಚೆಕ್‌ಔಟ್ | చెక్‌అవుట్ | செக்அவுட் |
| pickup | ಪಿಕಪ್ | పికప్ | பிக்அப் |
| delivery | ಡೆಲಿವರಿ | డెలివరీ | டெலிவரி |
| stock | ಸ್ಟಾಕ್ | స్టాక్ | ஸ்டாக் |
| credits | ಕ್ರೆಡಿಟ್ | క్రెడిట్‌లు | கிரெடிட் |
| shop | ಅಂಗಡಿ | దుకాణం | கடை |
| item (in prose) / item (in counts) | ವಸ್ತು / ಐಟಂ | వస్తువు / ఐటమ్ | பொருள் / ஐட்டம் |
| basket | ಬಾಸ್ಕೆಟ್ | బాస్కెట్ | பாஸ்கெட் |
| offers | ಆಫರ್ | ఆఫర్‌లు | ஆஃபர் |
| account | ಖಾತೆ | ఖాతా | கணக்கு |
| confirmed (stock) | ಖಚಿತಪಡಿಸು | నిర్ధారించు | உறுதிசெய் |
| accept (order) | ಒಪ್ಪಿಕೊಳ್ಳು | అంగీకరించు | ஏற்றுக்கொள் |
| cancel | ಕ್ಯಾನ್ಸಲ್ | క్యాన్సిల్ | ரத்து |

### Strings I was unsure of (all three languages)

| Key | English | Question |
|---|---|---|
| `orders.status.placed` | Waiting for shop | Shown in a status pill. kn ಅಂಗಡಿಯ ಉತ್ತರಕ್ಕೆ ಕಾಯುತ್ತಿದೆ is long; te దుకాణం జవాబు కోసం and ta கடையின் பதிலுக்காக are shorter fragments ("for the shop's reply"). Check that each fits at 360 px and reads as a status. |
| `product.confirmed`, `search.noneConfirmed.title` | Confirmed {time} / None of the shops has confirmed yet. | kn {time} ಖಚಿತಪಡಿಸಲಾಗಿದೆ, te {time} నిర్ధారించారు, ta {time} உறுதிசெய்யப்பட்டது. These are formal words for "the shop confirmed the stock" (D-190); a spoken word may be clearer. |
| `orders.code.note`, `orders.ready.note` and other `{shop}` lines | Show this code at {shop}. | The case ending after `{shop}` is a separate word (kn `{shop} ನಲ್ಲಿ`, te `{shop} లో`, ta `{shop} இல்`). Joined forms are more natural but cannot be written for a name we do not know. |
| `store.inStoreOnly`, `store.inStoreOnly.banner` | In-store only / This shop sells in store only… | kn ಅಂಗಡಿಯಲ್ಲಿ ಮಾತ್ರ, te దుకాణంలో మాత్రమే, ta கடையில் மட்டும். The banner repeats "shop" ("this shop sells only in the shop"). It may read as a location, not "no online orders". |
| `orders.status.rejected` | Declined | kn ನಿರಾಕರಿಸಲಾಗಿದೆ, te తిరస్కరించారు, ta நிராகரிக்கப்பட்டது. Formal and may sound blunt. |
| `orders.status.collected` | Collected | kn ತೆಗೆದುಕೊಳ್ಳಲಾಗಿದೆ, te తీసుకున్నారు, ta பெறப்பட்டது. Check the sense: "you picked it up". |
| `basket.preview` | Totals are a preview. Checkout shows the final price. | kn ಅಂದಾಜು, te అంచనా, ta தோராயமானவை ("estimate"). As with Hindi, this may make the price sound unreliable. |
| `basket.name.placeholder` | Weekly groceries | kn ವಾರದ ದಿನಸಿ, te వారపు సరుకులు, ta வாராந்திர மளிகை. Check that each sounds like a name someone would give a list. |
| `basket.ask.example1`–`example4` | Aashirvaad atta 5 kg, … | Kept in English in all three, as in Hindi: shops list item names in English. |
| `auth.code.resendIn`, `home.radius.km` | Resend in {seconds} s / {km} km | "s" and "km" kept in Latin. The local words for seconds and kilometres may be clearer. |
| `cart.count`, `basket.list.count`, `orders.row.meta` | `{count, plural, one {# item} other {# items}}` | kn ಐಟಂ / ಐಟಂಗಳು, te ఐటమ్ / ఐటమ్‌లు, ta ஐட்டம் / ஐட்டங்கள். Check the loanword plural forms. |
| `cart.credits.hint`, `cart.credits.valueText`, `cart.credits.using` | `{count, plural, one {# credit} other {# credits}}` | Tamil uses கிரெடிட் in both branches (after a number the singular form is usual). Kannada and Telugu change the noun. |
| `cart.credits.blocked.below_credit_minimum` | Credits can be used when your items come to the shop's minimum for credits. | kn ಕನಿಷ್ಠ ಮೊತ್ತ, te కనీస మొత్తం, ta குறைந்தபட்சத் தொகை for "minimum amount": formal; check that the rule is still clear. |

### Strings I was unsure of (one language)

| Language | Key | Text | Question |
|---|---|---|---|
| Kannada | `orders.accepted.note` | {shop} ನಿಮ್ಮ ಆರ್ಡರ್ ಒಪ್ಪಿಕೊಂಡಿದೆ ಮತ್ತು ನಿಮಗಾಗಿ ಇಟ್ಟುಕೊಂಡಿದೆ. | Neuter verb ending for a shop name. Check it reads naturally. |
| Kannada | `basket.undo` | ರದ್ದುಮಾಡಿ | Same word family as "cancel" (ರದ್ದು). Could be confused with cancelling. |
| Telugu | `home.map.live` | మీ దగ్గర, ఇప్పుడు | A small badge; the "live" sense is approximate (as in Hindi). |
| Telugu | `shell.nav.label` | ముఖ్య | Screen-reader label for the main navigation. It may need a noun (ముఖ్య మెనూ). |
| Tamil | `basket.undo` | செயல்தவிர் | The formal software term. A spoken alternative may be clearer. |
| Tamil | `shell.nav.label` | முதன்மை | Screen-reader label for the main navigation; same question as Telugu. |
| Tamil | `checkout.place` | ஆர்டர் செய் · {total} | Plain imperative on the button; a polite form (ஆர்டர் செய்யுங்கள்) is longer. |
