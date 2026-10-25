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
- `make gen` (`pnpm --filter @todayz/i18n gen`) writes `compiled/<app>/{en,en-XA,hi,kn,te,ta}.json` as ICU ASTs (git-ignored). A language file has the reviewed translation where there is one and English everywhere else. Machine drafts and stale translations never ship to prod; with `TODAYZ_I18N_DRAFTS=1` (set by `make dev` and `make e2e`, never by the Pages build) gen also ships drafts. See [docs/translation-workflow.md](docs/translation-workflow.md).
- Load a catalogue per app with `import('@todayz/i18n/compiled/customer/hi.json')` and `asMessages()`; hand it to react-intl:

  ```tsx
  <IntlProvider locale={intlTag(locale)} messages={messages} timeZone="Asia/Kolkata">
  ```

  `intlTag(locale)` is `<lang>-IN-u-nu-latn` (Latin digits in every language); `timeZone` keeps any react-intl date formatting in IST. Use `htmlLang(locale)` only for `<html lang>`.
- In code use literal ids so the lint can see them: `intl.formatMessage({ id: 'shell.nav.home' })`, `<FormattedMessage id="..." />`, `defineMessage`, `defineMessages({ a: { id: '...' } })` or `t('site.home.title')` (the site's helper).
- Messages must not use `{x, number}`, `{x, date}` or `{x, time}`: amounts, distances and times arrive pre-formatted (`formatMoney`, `formatDistance`, `formatTime`) as plain `{x}`. Plural `#` is fine.

### Lint (`make check` step `check-i18n-messages`, wired in `mk/42-i18n.mk`)

The package never reads sibling folders itself: the caller passes each app's source folder, `node scripts/lint.mjs --src customer=../../customer/src --src site=../../site/src ...` (`pnpm --filter @todayz/i18n lint:messages` does all four and lists warnings). Every app with a catalogue needs a `--src`; a folder that is missing or holds no source files is an error, so the lint cannot pass by looking at nothing. Test files (`*.test.*`, `*.spec.*`), `*.d.ts`, `*.gen.ts`, `gen/`, dot-folders and `node_modules` are not scanned.

Errors:
- an id used in code that is not in the app's English files (the missing-key lint). Code is parsed with the TypeScript parser (`.astro` files are scanned for quoted strings). Ids are collected from `formatMessage({ id })`, `defineMessage`, `defineMessages({ ... })`, `<FormattedMessage id=... />` (attribute order and nested `values={{ b: (c) => <b>{c}</b> }}` do not matter) and `t('...')`, and also from any other string literal that looks like a key: `ns.part[.part]` made of letters, digits, `_` or `-`, whose first segment is one of that app's catalogue namespaces (`shell`, `site`, ...) and that does not end in a file extension. This catches `label: 'shell.nav.serch'` held in data. Import/export paths and type literals are skipped. If a plain string such as `'shell.config'` is flagged by mistake, rename it or move it out of the catalogue's namespace;
- an English message that does not parse or uses number/date/time arguments;
- a translation for an unknown key; one that does not parse, uses number/date/time arguments or takes different arguments than English (names, plural/select, tags); an empty message; a review flag other than `machine` or `reviewed`; a duplicate key.

Warnings (counted; `lint:messages` lists them): untranslated keys, machine drafts, stale translations (English changed since; these skip the argument check), English keys with no literal use. `make gen` ships a translation only if it parses and takes the same arguments and tags as English; otherwise it ships English and warns, and never aborts.

## Language detection and persistence

```ts
const store = browserStore(); // localStorage, or undefined when access throws (blocked storage, private mode, SSR)
const locale = detectLocale({ store, search: location.search, languages: navigator.languages, allowPseudo: pseudoAllowed(mode) });
await applyLocale(locale);   // <html lang> + font loading
saveLocale(store, 'hi');     // when the person picks a language (key "todayz.lang")
hasChosenLocale(store);      // false -> show the C-01 language screen
```

Do not touch `localStorage` directly: reading the property can throw. Order: stored choice, `?locale=`, browser languages, English. `?locale=pseudo` works only with `allowPseudo` and is never stored. Storage failures are swallowed. `hi-Latn` (any `-Latn` Indic tag, Hindi written in Latin letters) maps to English.

URL-only steps: `resolveLocale(search, opts)` is the pseudo-locale switch only (`?locale=pseudo` with `allowPseudo`, anything else `en`; the apps use it); `resolveLanguage(search, opts)` also accepts a language code (`?locale=hi`).

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

Amounts are integer paise everywhere; a fractional paise throws. English months are fixed three-letter names (`Sep`, never `Sept`).

Times: a `Date`, a number (epoch ms) or an ISO 8601 string **with `Z` or an offset** (`2026-10-22T13:10:00Z`, `...+05:30`). A string without one (`2026-10-22T18:40:00`, `2026-10-22`) throws a `RangeError`: JavaScript would read it in the device's own zone, so the same string would show different times on different phones.

## Fonts

`@todayz/tokens` ships the self-hosted `@font-face` rules (Anek Latin/Devanagari/Kannada/Telugu/Tamil, Figtree, Noto Sans Devanagari/Kannada/Telugu/Tamil), one `unicode-range` subset per script, so a script's file downloads only when that script is on the page. `applyLocale` / `loadFonts(locale)` ask the browser for the language's faces up front (script sample text) to avoid a late swap; failures fall back to `system-ui`. `fontFamiliesFor(locale)` and `FONT_FAMILIES` name the families.

## Pseudo-locale en-XA

Accented letters, wrapped in `[ ]`, at least 40% longer; placeholders, plurals and tags keep working. Dev, test and `--mode e2e` builds only.
