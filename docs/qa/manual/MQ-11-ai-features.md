# MQ-11 AI features

Every AI feature returns a draft. The tester feeds each one an input that the mock model answers wrongly on purpose and checks that nothing changes until a person confirms, that the person can fix or drop each line, and that a feature whose flag is off has no entry point at all.

- **Covers:** 08 §5 MQ-11 (list search, voice search, bill scan, shelf photo, review reply draft, ticket triage); 11 §1 rules 1, 2, 5, 7, 10; E2E-11, E2E-13, E2E-14 manual side; VO-01, VO-02; SC-AI coach k ≥ 10 (D-71).
- **Screens:** C-05, C-07, C-09, C-22, C-23; B-02, B-03, B-06, B-07, B-11, B-17, B-18, B-19; A-08, A-13, A-14, A-17.
- **Devices:** customer P390 (profile A); shop phone P360 (profile B) and shop desktop D1280 (profile C); admin D1440 (profile D).
- **Time:** about 90 minutes.

## Preconditions

1. `make reset`; `clock reset`; run between 10:00 and 18:00 IST.
2. Mocks in fixture mode (`MOCK_LLM` unset). `make env-check` shows `ANTHROPIC_API_KEY missing` and `SARVAM_API_KEY missing`: in `local` the mocks count as configured (11 §1 rule 2).
3. All AI flags start **off** (01 §17 default). The script turns each one on in its section.
4. `<seed:ai-fixtures>` provides these mock replies. The mocks LLM tab shows which fixture answered each call (`X-TZ-Mock-Fixture`).

   | Fixture | Input it matches | Reply |
   |---|---|---|
   | `list_search/qa-four-items` | "1 kg atta, 2 Nandini milk, Dolo 650, maggi" | 4 lines: Wheat atta 5 kg ×1 (wrong pack on purpose), Nandini Toned Milk 500 ml ×2, Dolo 650 strip ×1, Maggi 70 g ×1 |
   | `voice_stock/qa-mixed` | STT transcript "paanch Maggi bik gaye, Coca-Cola khatam, das atta aaya, butter ka daam saath" | 4 lines: `sale` Maggi ×5, `count` Coca-Cola 0, `restock` atta ×10, `set_price` Amul Butter ₹60 |
   | `bill_scan/qa-supplier-bill` | the QA bill photo | 3 lines: Amul Butter ×24 cost ₹50 expiry +20 d; Curd 400 g ×12 cost ₹28 expiry +3 d; "Tata Salt 1 kg" ×10 (no match) |
   | `shelf_scan/qa-shelf` | the QA shelf photo | 5 products, one of them a wrong brand on purpose |
   | `review_reply/qa-reply` | a 2-star review "Butter was expired" | a polite reply that contains the number "20% off" (not in the input) |
   | `ticket_triage/qa-ticket` | the seeded open customer ticket | category "Order issue", priority high, linked order guess, a draft reply |
   | `mock_overloaded/default` | any call while the tester forces it | 529 overloaded |

5. STT fixture for the voice clip in section D, keyed by the audio hash.

## Steps

### A. Flags off hide every entry point

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.01 ★ | With all AI flags off, open the customer app Home and Search (profile A). | No "Shopping list" entry and no mic in the search field. No broken button or empty slot. | `R-17.3` 11 §1 C-03 C-05 | ▢ | |
| MQ-11.02 | In profile C (O20) open Overview, Stock, Reviews and the onboarding starter catalogue preview. | No coach AI text (template text only), no "Scan supplier bill", no "Shelf photo", no "Draft reply". Voice update shows only if `feature_voice_enabled` and `ai_voice_stock_enabled` are both on: it is hidden now. | `R-17.3` B-03 B-06 B-07 B-11 | ▢ | |
| MQ-11.03 | In profile D (AD47) open a ticket in A-14, a pending product in A-08 and a flag in A-13. | No assist panel in any of them. | `R-17.3` A-08 A-13 A-14 | | |
| MQ-11.04 | As C11 call `POST /v1/me/ai/list-search/drafts` with any list (README §5). | 404: the feature is hidden while its flag is off (04 §2.8). Nothing in the mocks LLM tab. | `R-17.3` 04 §2.8 | | |
| MQ-11.05 | As SA48 in A-17 turn on `ai_listparse_enabled`, `ai_querynorm_enabled`, `ai_voice_stock_enabled`, `ai_billscan_enabled`, `ai_shelfscan_enabled`, `ai_replydraft_enabled`, `ai_triage_enabled`, `ai_catalogue_enabled`, `ai_riskexplain_enabled`, `ai_coach_enabled`, `ai_insighttext_enabled` with reason "QA MQ-11.05". | Each saved with an audit row. The entry points appear after a refresh in each app. | `R-17.3` A-17 | ▢ | |

### B. Shopping list search (AI-C1, C-22)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.06 ★ | Profile A, C11, QA point, 2 km. Open Shopping list (C-22). Type "1 kg atta, 2 Nandini milk, Dolo 650, maggi" and search. | A list of 4 parsed lines shows first, each editable, before any cart change. Atta shows "Wheat atta 5 kg ×1". | 11 §3 AI-C1 C-22 | ▢ | |
| MQ-11.07 | Look at the shop ranking. | Shops ranked by how much of the list they stock, then price, then distance, in the form "Sri Lakshmi has 4 of 4, 0.6 km, ₹337" (₹245 + ₹48 + ₹30 + ₹14). The cart is still empty. | 11 §3 AI-C1 C-22 | ▢ | |
| MQ-11.08 | Remove the atta line (the wrong pack). | Ranking recomputes for 3 lines: "Sri Lakshmi has 3 of 3, 0.6 km, ₹92". No model call is made for the edit (mocks LLM tab unchanged). | 11 §1 rule 1 C-22 | | |
| MQ-11.09 ★ | Tap "Fill cart" for Sri Lakshmi. | The cart (C-09) holds exactly Nandini milk ×2, Dolo 650 ×1, Maggi ×1, ₹92. Nothing was placed. | E2E-13 C-09 | ▢ | |
| MQ-11.10 | Place the order (pickup, no credits) and cancel it with "Changed my mind". | Normal order flow; the list search did not change any price or stock. | E2E-13 | | |
| MQ-11.11 | Open the mocks LLM tab, open the last list-search request. | The request text holds the list only; no phone, name, address or order code. | 11 §1 rule 6 OB-02 | | |

### C. Voice search (AI-C2, C-23)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.12 | Search "doodh" by typing. | Milk results (synonym table), and the mocks LLM tab shows no call (table first, model only when the table misses). | 11 §3 AI-C2 C-05 | | |
| MQ-11.13 | Tap the mic (C-23), allow the microphone, play the STT fixture clip "ಹಾಲು" (or speak if a fixture exists for your voice). | The heard words show in the field before results load; the tester can edit them. Results for milk. | 11 §3 AI-C2 C-23 | ▢ | |
| MQ-11.14 | Deny the microphone permission and tap the mic again. | A short message explains how to allow the mic; typing still works. | C-23 | | |

### D. Voice stock update (AI-R1, B-06)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.15 ★ | Profile B as K22 (cashier). Open Voice update (B-06). Hold to talk and play the `voice_stock/qa-mixed` clip. | Processing state, then 4 draft lines: `sale` Maggi ×5, `count` Coca-Cola 0, `restock` atta ×10, `set_price` Amul Butter ₹60, each with a confidence. | `R-12.7` AI-R1 B-06 | ▢ | |
| MQ-11.16 | Before confirming, open Stock (B-07) in profile C. | Maggi 50, Coca-Cola 12, atta 6, Amul Butter price ₹58. Nothing changed (VO-02). | VO-02 `R-12.7` B-07 | | |
| MQ-11.17 ★ | Back in B-06 as K22. | `restock` and `set_price` lines show disabled with "Ask a manager". `sale` and `count` lines are enabled. | VO-01 D-65 B-06 | ▢ | |
| MQ-11.18 | Change the Maggi line quantity from 5 to 4. Confirm. | Applied: Maggi on hand 46 (`sale_pos`, no customer), Coca-Cola 0 (`count_correction`). Atta and butter unchanged. The draft records Maggi as "edited". | VO-01 `R-4.4` B-07 | ▢ | |
| MQ-11.19 | Force the `set_price` line through the API: K22's `POST {S}/ai/voice-stock/drafts/<id>/confirm` with that line's outcome `accepted`. | 403 for that line. Butter stays ₹58. | VO-01 D-65 04 §2.8 | | |
| MQ-11.20 | Sign in as M21 (manager), open the same draft from the drafts list. Confirm the two remaining lines. | Atta on hand 16 (`restock`), Amul Butter ₹60. The customer shop page shows "updated just now" on atta. | VO-01 E2E-11 C-07 | ▢ | |
| MQ-11.21 | Record a new clip, get its draft, then tap Discard. | Draft discarded; nothing changed. | VO-02 B-06 | | |
| MQ-11.22 | In mocks force `mock_overloaded` and record a clip. | An error with "Try again"; no draft lines; nothing changed. | G1 B-06 | ▢ | |

### E. Supplier bill scan (AI-R2, B-17)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.23 ★ | Profile C as M21. Stock → Scan supplier bill (B-17). Upload the QA bill photo. | 3 lines: Amul Butter ×24 cost ₹50 expiry, Curd ×12 cost ₹28 expiry in 3 days, "Tata Salt 1 kg" ×10 highlighted as no match with "Create product". | AI-R2 B-17 | ▢ | |
| MQ-11.24 | Leave the page without confirming. Open B-07. Then as K22 call `GET {S}/ai/supplier-bill/drafts/<id>`. | Butter and curd on hand unchanged; no new batches. K22 (cashier) cannot read or confirm a supplier-bill draft: record the status (manager feature, 04 §2.8). | 11 §1 rule 1 04 §2.8 B-07 | | |
| MQ-11.25 | Return to the draft. Change butter to 20. Remove the salt line. Confirm. | Butter +20 (`supplier_bill`) with a batch and expiry; curd +12 with a batch expiring in 3 days. Salt not created. | AI-R2 `R-4.4` E2E-14 B-07 | ▢ | |
| MQ-11.26 | Open Overview (B-03) Demand insights. | "Going to waste" reads "**1 item expires within 5 days**: Curd 400 g." (the batch expiring in 3 days) with the action **Flash offer →**. | E2E-14 `R-12.8` IN-05 B-03 | ▢ | |
| MQ-11.27 | Tap Create offer → a flash offer 20% off curd for today. Publish. In profile A open Sri Lakshmi. | The curd offer shows with the new price. | E2E-14 `R-12.2` C-07 | ▢ | |

### F. Shelf photo onboarding (AI-R3, B-18)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.28 | Sign in as N32 on a new profile and start the wizard (B-02) to the starter catalogue. Choose "Shelf photo" (B-18) and upload the QA shelf photo. | A checklist of 5 recognised products, matched to the master catalogue, prices defaulting to MRP, all unticked or ticked but not yet added. | AI-R3 B-18 | ▢ | |
| MQ-11.29 | Untick the wrong-brand product. Add the rest. | 4 items added at MRP. The wrong one is not in the store. | AI-R3 B-18 | | |

### G. Review reply drafts (AI-R6, B-11)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.30 | As C10, review a collected Sri Lakshmi order: 2 stars, "Butter was expired". As O20 open B-11 and tap "Draft reply". | A draft appears in the reply box, labelled as a suggestion. Nothing is posted. | AI-R6 `R-10.2` B-11 | ▢ | |
| MQ-11.31 | Read the draft. | Record whether the "20% off" text shows. The grounding rule says numbers not in the input are rejected; a reply draft with an invented offer must not show it, or must show a clear warning to edit. | 11 §1 rule 5 B-11 | ▢ | |
| MQ-11.32 | Edit the text and post. | The posted reply is the edited text. The customer sees it on C-07 reviews. | AI-R6 `R-10.2` C-07 | | |

### H. Shop coach (AI-R5, B-19)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.33 | `clock set <tomorrow>T06:00:30+05:30` (the 06:00 coach job). As O20 open Overview. | Up to 3 newest coach cards on top; the feed (B-19) has no more than 5 new cards today (`coach_max_cards_per_day`). Each card has type, title, numbers and one action. | AI-R5 `R-12.8` B-19 | ▢ | |
| MQ-11.34 | Find the "Wanted nearby" card. | "12 people near you searched for **Horlicks 500 g**" (12 ≥ 10), the same number as the Wanted nearby row on B-03 (one computation, 11 §2.1). No card for Bournvita (9 searches, below `insight_min_searches` 10). | `R-16.4` D-71 B-19 | ▢ | |
| MQ-11.35 | Tap a card's action (for example Restock). | It opens the restock screen prefilled. Nothing is applied until the person saves. | 11 §2.1 B-19 | | |
| MQ-11.36 | Mark one card "Not useful". | It leaves the feed; the same topic does not return for 7 days (`coach_dismiss_quiet_days`). | AI-R5 B-19 | | |
| MQ-11.37 | Sign in as K22 (cashier). | No coach feed (owner and manager only). | B-19 | | |
| MQ-11.38 | `set ai_coach_enabled = false`. Run the job again (`clock +1d`). | Cards still show with template text (no AI wording); no model calls in the mocks. | 11 §1 AI-R5 B-19 | | |

### I. Admin assists (AI-A1, AI-A2, AI-A3)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.39 ★ | As S42 open the seeded open ticket in A-14. | The assist panel shows category, priority, a linked order guess and a reply draft. Nothing is sent and the ticket fields are unchanged until the agent applies them. | AI-A2 A-14 | ▢ | |
| MQ-11.40 | Change the category to a different one, edit the reply, send. | The ticket uses the agent's category and text. The draft records "edited". | AI-A2 A-14 | | |
| MQ-11.41 | As G43 open the seeded pending product in A-08. | The assist proposes a normalised name and a duplicate to merge into with a reason. No merge happens until G43 clicks Merge. | AI-A1 A-08 | ▢ | |
| MQ-11.42 | As AD47 open the seeded `store_strikes` flag in A-13. | A short evidence summary shows. It does not clear or confirm the flag. | AI-A3 A-13 | | |

### J. Caps and cost

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-11.43 | `set ai_daily_calls_customer = 2`. As C11 run 3 list searches. | Third one refused with a clear message; no model call. `429 ai_daily_cap_reached` with `extensions.scope` `customer`, a `Retry-After` header, and the copy "AI help is paused until tomorrow." The entry point is disabled, with no retry button. | `R-17.3` 11 §1 rule 7 AI-01 | ▢ | |
| MQ-11.44 | `clock set <tomorrow>T00:00:05+05:30`. Search again. | Allowed again (the cap is per IST day). | `R-17.3` | | |
| MQ-11.45 | Open Grafana dashboard 14 "AI quality and cost". | Calls by feature (list_search, voice_stock, bill_scan, shelf_photo, review_reply, ticket_triage, catalogue_cleanup, risk_summary, coach), acceptance rate per feature with this run's accepted, edited and discarded drafts, and cost per day. | 05 §4 dashboard 14 | ▢ | |
| MQ-11.46 | `set ai_daily_cost_cap_paise = 100` (₹1) as SA48. Run list searches until the mock cost passes ₹0.80. | The "AI cost" alert fires at 80% in Grafana alerting (local contact point). At 100%, AI entry points show the "unavailable" state and no further model calls happen until the next IST day. | `R-17.3` D-70 05 §5 | ▢ | |

## Cleanup

1. `restore defaults`: every AI flag back to false; `ai_daily_calls_customer` = 30; `ai_daily_cost_cap_paise` = 50000.
2. `clock reset`; `make reset`.
