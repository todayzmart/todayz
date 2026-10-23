# Translation workflow

English is the source. Hindi, Kannada, Telugu and Tamil follow in two steps: a machine draft, then a human review. Each key carries its own flag, so a screen can ship with some keys reviewed and the rest in English.

## States of a key (per language)

| State | In the file | What people see |
|---|---|---|
| Not translated | no entry | English |
| Machine draft | `"review": "machine"` | English (drafts do not ship) |
| Reviewed | `"review": "reviewed"` | the translation |
| Stale | `enHash` differs from the current English | English, until re-translated and re-reviewed |

`enHash` is a short fingerprint of the English message the translation was made from. Changing the English of a key therefore sends every language back to "not shipped" until it is translated again, so a translation never describes text that no longer exists. Changing only a `description` does not.

## Steps

1. A WP adds English in `messages/<app>/<area>.en.json` (`defaultMessage` plus a `description` saying where it appears and any constraint). Sentence case, no exclamation marks, product names untouched. Plurals use ICU (`{n, plural, one {# item} other {# items}}`); Hindi, Kannada, Telugu and Tamil need the `one` and `other` options (other categories only if the language needs them).
2. List what needs work: `node shared/i18n/scripts/translations.mjs todo <app> <lc>` prints JSON `[{ key, area, en, description, was? }]`.
3. Machine draft: give that list to the translation model or service with the glossary below, get `{ "key": "translated message" }` back, and import it: `... import <app> <area> <lc> drafts.json`. The import checks that the draft takes the same arguments as English and stores it as `machine`. A model output is a draft that a person confirms; it never ships on its own.
4. Human review: a fluent reviewer reads the drafts in context (use `?locale=<lc>` with `--drafts` output, or the draft file), edits the `message`, and approves: `... approve <app> <area> <lc> key1 key2`. Approve refuses a key whose English changed since the draft.
5. `pnpm --filter @todayz/i18n lint:messages` (and the `check-i18n-messages` step of `make check`) must pass. It reports reviewed, machine, missing and stale counts per language. A stale translation (English changed) only warns; the other checks (parse, same arguments and tags, no number/date/time arguments) are skipped for it because it will be translated again. `make gen` ships a reviewed translation only if it is not stale, parses and takes the same arguments and tags as English; otherwise English ships and gen warns.

## Glossary and style

- Keep as written: TodayZ, UPI, KYC, PIN, OTP, GST, product names, ₹ amounts (placeholders carry them), shop names.
- Digits stay Western (4827). Dates and amounts come from placeholders formatted by the formatters, never typed into a message.
- Translations run 20 to 40% longer; never rely on a fixed width. No uppercase and no letter spacing for Indic scripts.
- Use the polite register ("aap"-form in Hindi, equivalent in the others), short and spoken, as a shop counter would say it.
- Placeholders keep their names (`{name}`), tags keep theirs (`<b>`). The lint fails otherwise. `{x, number}`, `{x, date}` and `{x, time}` are not allowed in any language; plural `#` is.
- "Stock", "order", "credits", "pickup" and "delivery" take one fixed term per language; record the term here when the first reviewer picks it.

## Review checklist

Meaning matches the English and its `description`; the plural forms read right at 0, 1, 2 and 21; nothing is cut off at the pseudo-locale width; the term for each glossary word is the one already used elsewhere in the app.
