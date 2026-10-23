# W1-03 i18n and formatting: QA report

Branch `worktree-agent-a92a4e4c21d3d8cf5`, built from main `d402367`, rebased by the lead onto `e286716` (one doc conflict in `shared/CLAUDE.md`, both sides kept). Commits after the rebase: 2a8b63c, 7ffb03b, 6146a1c, 86b8ce1 (review fixes).
After the rebase: 197 tests pass, typecheck and lint are clean, and the message lint reports only the expected untranslated-site warnings.

## What was built (`shared/i18n`)

- FormatJS/ICU catalogues per app and area: `messages/<app>/<area>.<lang>.json` with `{message, review, enHash}`. `make gen` compiles them to ASTs in `compiled/`, which is git-ignored.
- Pseudo-locale `en-XA`:
  - at least 40% longer;
  - accented, including inside plural, select and tag children;
  - placeholders, `#` and tags kept intact.
- Formatters:
  - `formatMoney(paise, 'dashboard'|'product'|'receipt')`: Indian grouping, with paise shown only when they are not zero. Receipts always show two decimals.
  - `roundToRupee`: rounds half up; negatives round the same way.
  - `formatDistance(metres)`: under 50 m reads "0.1 km", one decimal below 10 km, then whole km.
  - `formatRelative`, `formatAbsolute`, `formatTime(then, now, locale, {relativeWithinMs})`: IST is a fixed +5:30 shift, independent of the device's time zone. "Today" and "Yesterday" are decided by IST date, and every language uses Latin digits (`nu-latn`).
  - Money and distance always use `en-IN-u-nu-latn`, because Node's kn data groups digits in thousands.
- Fonts per script: Anek, Figtree and Noto Sans subsets.
- Language detection and persistence for en, hi, kn, te and ta:
  - order: stored choice, then URL, then browser languages, then English;
  - storage key `todayz.lang`;
  - `-Latn` Indic tags fall back to English.
- Missing-key lint (`mk/42-i18n.mk`, `check-i18n-messages`):
  - extraction uses the TypeScript parser and covers `defineMessages`, `t('site.x')`, `<FormattedMessage>`, and key-shaped strings in data;
  - each app's `src` is passed in with `--src`; a missing or empty folder is an error;
  - `{x, number|date|time}` arguments are refused;
  - stale translations produce a warning, not an error.
- Translation workflow (`docs/translation-workflow.md`):
  - every key starts as a machine draft and needs a human review flag;
  - machine drafts and stale entries never ship;
  - gen ships a translation only if it parses and its arguments and tags match English.

## Review

A fresh read-only review (claude-reviewer, Opus high) of d22f201 found no Critical or High issues. Its 5 Medium and 5 Low findings were all fixed in 85fb54d:

1. `resolveLocale` is pseudo-only again. Real languages go through the new `resolveLanguage`, which the W1-08 app shells will wire in.
2. The lint no longer reads sibling folders from inside `shared/`, and fails instead of passing when it has nothing to check.
3. The staleness check runs before the argument check, so editing English no longer blocks the build.
4. Lint holes closed: `defineMessages`, the site's `t()`, ids held in data, and `FormattedMessage` with nested JSX.
5. `browserStore()` guards `localStorage` access when the browser blocks storage.
6. ICU number, date and time arguments are banned. The README `IntlProvider` uses `intlTag` and Asia/Kolkata.
7. gen falls back to English for a translation that doesn't parse or doesn't match English, and never aborts.
8. English months are three letters ("Sep", not "Sept").
9. ISO strings without `Z` or an offset are refused.
10. `hi-Latn` falls back to English.

The reviewer found these correct: lakh and crore grouping, negatives, zero, rounding at .5, very large numbers, rejection of NaN and fractional paise, IST across the date line on any device, plurals, detection, HTML and rich-text safety, safety for server-side rendering and Astro, and no dependency or lockfile changes.

## Checks

- shared/i18n: 197 tests pass, including under `TZ=America/Los_Angeles`. eslint and tsc are clean.
- `make check-i18n-messages`: 0 errors for all four apps.
- Full `make check` in the worktree: see the merge note below.

## Open questions for the founder

1. When does relative time ("12 min ago") switch to absolute ("Today, 6:40 pm")? The suggested default is under 60 minutes. For now the caller passes the cut-off in.
2. Dashboard paise: show them only when they aren't zero (the current behaviour, e.g. ₹289.50), or always round to whole rupees?
3. No translations exist yet. Every hi, kn, te and ta string needs a native reviewer.
