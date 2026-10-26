# Admin console languages (Hindi, Kannada, Telugu, Tamil): review

Claude machine-wrote the admin translations in `shared/i18n/messages/admin/*.{hi,kn,te,ta}.json`. They are released on the founder's standing instruction (25 Oct) without a native-speaker review.

**Reviewer: none yet (native speaker needed). Approver: founder.**

## Approval record

| Date | Approver | What | Note |
|---|---|---|---|
| 2026-10-26 | founder (Rahul), standing 25 Oct instruction "go live with hindi … and all other languages too", relayed by the lead | W3-13 admin AI: the 64 `aiAssist.*` keys in hi, kn, te and ta (A-08 catalogue clean-up, A-14 ticket triage, A-13 risk summary) | Machine-written; `translations.mjs approve` set `review: reviewed`. No native-speaker review. Placeholders (`{line}`, `{name}`, `{similarity}`) are unchanged; "AI" stays as written. Strings to check: `aiAssist.risk.ungrounded` (says a number is not in the evidence) and `aiAssist.catalogue.mergeOrEdit`. |
| 2026-10-26 | same | W3-06 `field.items.notMine` ("Added by someone else") in hi, kn, te and ta | Same instruction; no native-speaker review. |

`translations.mjs approve` only sets `review: reviewed` in the catalogue. This table is the record of who approved and when.
