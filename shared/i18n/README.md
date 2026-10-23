# @todayz/i18n

Message catalogues, the en-XA pseudo-locale, formatters, language detection and fonts for every TodayZ app. Owned by W1-03; per-area message files are owned by the WP that owns the screen area.

```ts
import { createFormatters, detectLocale, saveLocale, applyLocale, formatMoney } from '@todayz/i18n';
```

## Languages

`LOCALES = ['en','hi','kn','te','ta']` (English is the default and the fallback for every key). `en-XA` (`PSEUDO_LOCALE`) is the pseudo-locale and is never offered to people. `NATIVE_NAMES` has each language in its own script for LanguagePicker. `htmlLang(locale)` gives the `<html lang>` value (`en-IN`, `hi`, `kn`, `te`, `ta`, `en-XA`).

## Messages

- English source: `messages/<app>/<area>.en.json`, `{ "key": { "defaultMessage": "...", "description": "..." } }`, ICU MessageFormat, sentence case, no exclamation marks.
- Translations: `messages/<app>/<area>.<hi|kn|te|ta>.json`, `{ "key": { "message": "...", "review": "machine"|"reviewed", "enHash": "ab12cd34" } }`. See [docs/translation-workflow.md](docs/translation-workflow.md).
- `make gen` (`pnpm --filter @todayz/i18n gen`) writes `compiled/<app>/{en,en-XA,hi,kn,te,ta}.json` as ICU ASTs (git-ignored). A language file has the reviewed translation where there is one and English everywhere else. Machine drafts and stale translations never ship; `node scripts/gen.mjs --drafts` includes drafts for a local look.
- Load a catalogue per app with `import('@todayz/i18n/compiled/customer/hi.json')` and `asMessages()`; hand it to react-intl with `locale={htmlLang(locale)}`.
- In code use literal ids so the lint can see them: `intl.formatMessage({ id: 'shell.nav.home' })` or `<FormattedMessage id="..." />` (`defineMessage` works too).

### Lint (`pnpm --filter @todayz/i18n lint`, runs in `make check`)

Errors: an id used in an app's `src/` that is not in that app's English files (the missing-key lint); a translation for an unknown key; a translation that does not parse or takes different arguments than English (names, plural/select, tags); an empty message; a review flag other than `machine` or `reviewed`; a duplicate key. Warnings (counted; `lint:messages` lists them): untranslated keys, machine drafts, stale translations, English keys with no literal use.

## Language detection and persistence

```ts
const locale = detectLocale({ store: localStorage, search: location.search, languages: navigator.languages, allowPseudo: pseudoAllowed(mode) });
await applyLocale(locale);        // <html lang> + font loading
saveLocale(localStorage, 'hi');   // when the person picks a language (key "todayz.lang")
hasChosenLocale(localStorage);    // false -> show the C-01 language screen
```

Order: stored choice, `?locale=`, browser languages, English. `?locale=pseudo` works only with `allowPseudo` and is never stored. Storage failures (private mode) are swallowed. `resolveLocale(search, opts)` is the URL-only step.

## Formatters

Numbers and money use English (India) rules in every language: Latin digits (`nu-latn`), lakh/crore grouping, `.` decimal mark. Times are IST with a 12-hour clock and `am`/`pm`, Latin digits, language-specific words and month names.

| Function | Example |
|---|---|
| `formatMoney(paise, style)` | `dashboard`/`product`: `₹1,24,500`, `₹289`, `₹289.50` (decimals only when paise are non-zero); `receipt`: `₹1,246.00` (always two decimals). Negative: `-₹289`. |
| `roundToRupee(paise)` | POS round-off, half rounds up: 10050 -> 10100; -10050 -> -10100 |
| `formatDistance(metres)` | `0.6 km`, `2.4 km`, `12 km` (one decimal under 10 km; never `0.0 km`) |
| `formatRelative(then, now, locale)` | `now`, `12 min ago`, `3 hr ago`, `1 day ago` (Hindi etc. in their words) |
| `formatAbsolute(then, now, locale)` | `Today, 6:00 pm`, `Yesterday, 6:40 pm`, `22 Oct, 6:40 pm`, `31 Dec 2025, 11:59 pm` |
| `formatTime(then, now, locale, { relativeWithinMs })` | relative while younger than the cut-off, absolute after. The cut-off has no platform-wide value in the plan, so the caller passes it. |
| `createFormatters(locale, now?)` | the above bound to a language |

Amounts are integer paise everywhere; a fractional paise throws.

## Fonts

`@todayz/tokens` ships the self-hosted `@font-face` rules (Anek Latin/Devanagari/Kannada/Telugu/Tamil, Figtree, Noto Sans Devanagari/Kannada/Telugu/Tamil), one `unicode-range` subset per script, so a script's file downloads only when that script is on the page. `applyLocale` / `loadFonts(locale)` ask the browser for the language's faces up front (script sample text) to avoid a late swap; failures fall back to `system-ui`. `fontFamiliesFor(locale)` and `FONT_FAMILIES` name the families.

## Pseudo-locale en-XA

Accented letters, wrapped in `[ ]`, at least 40% longer; placeholders, plurals and tags keep working. Dev, test and `--mode e2e` builds only.
