# site/

Astro marketing site (todayz.in; locally https://todayz.localhost). Static output, zero JS by default.

Read first: the root `CLAUDE.md`, `docs/plan/README.md`, `docs/plan/01-product-rules.md`, then 02-architecture.md §4.5; design/source.

## Run

- `make dev`, then open https://todayz.localhost
- `pnpm --filter @todayz/site build`: the production build. Every mode except `development` and `e2e` is gated like production (`src/lib/env.ts`): it fails unless `PUBLIC_API_URL` (a bare https origin, no path) and `PUBLIC_TURNSTILE_SITE_KEY` are set, it leaves machine-draft locales out (`scripts/drop-draft-locales.mjs` removes their OG images), and verify-dist then rejects a Turnstile test key, a local API or app origin, and any draft locale.
- `pnpm --filter @todayz/site build:e2e`: the local and test build (`--mode e2e`). Missing values fall back to `https://api.todayz.localhost` and Cloudflare's always-pass test key, and draft locales stay in with the banner and `noindex`; `make check` and the e2e suite use it.

## Rules for this folder

- Real HTML per route (no iframe wrapper); sitemap, robots, Open Graph, JSON-LD.
- Tokens only, no raw hex; light theme only.
- The lead form posts to `POST /v1/public/leads` (Turnstile protected).

## Layout (W1-09)

- `astro.config.mjs`: static output, dev port 5304 (strictPort), Host `todayz.localhost`, dev toolbar off. A small integration serves and copies the `@todayz/tokens` icons and exposes the favicon and theme-color tags as `virtual:todayz-head`.
- `src/lib/pages.ts`: the page registry (id, English path, locales, title and description keys, lead-form flag). Routes (`src/pages/[...slug].astro`), sitemap, hreflang, footer and tests all read it. Home and retailers exist in en, hi, kn, te and ta; every other page is English only.
- `src/i18n/`: loads the site's catalogues from `@todayz/i18n` (`shared/i18n/messages/site/<area>.<lc>.json`, owned by W1-09, checked by `make check-i18n-messages`). English files hold `{defaultMessage}` descriptors; translations hold `{message, review, enHash}`. Only literals and `{name}` arguments. A locale ships only when every key of `common`, `home` and `retailers` is `reviewed` and current (`isMachineDraft` fails closed); production builds (every mode except `development` and `e2e`) leave draft locales out entirely: no pages, sitemap entries, hreflang, switcher or footer links, OG images (`siteLocales`). The site does not import `@todayz/ui`.
- `src/layouts/Base.astro`: head tags (canonical, hreflang, Open Graph, Twitter, JSON-LD), skip link, header, machine-draft banner, footer. The Turnstile script is added only when the page holds the lead form.
- `src/views/`: one view per page kind (Home, Retailers, Info, Legal, NotFound). `src/components/`: Header (sticky glass), Footer (giant wordmark), LangSwitch, Faq, Section, Steps, Icon (Lucide, inline SVG), LeadForm. `src/components/home/`: the landing sections ported from the landing prototype (hero scene, marquee, problems, bento, showcase, demand insights, shoppers, shop types, three steps, idea band, testimonials, FAQ, CTA); `RetailersView` reuses them. `Testimonials` renders nothing while it has no real quotes.
- Bright look (D-260): `Base.astro` sets `<body data-skin="bright">` (the `[data-skin="bright"]` rules in `@todayz/tokens`) on `--page`; cards are white with `--shadow-card` and no outline. The shop types show round CC0 photos from `src/assets/photos/` (sources in `SOURCES.md`), imported in `components/home/data.ts`; `e2e/screens.spec.ts` loads lazy images before its full-page shots.
- `src/styles/landing.css`: the landing styles, every rule scoped under `.lp`. Mock screens are `aria-hidden` and their inline `height`/`width` percentages are the only inline styles. Animations are CSS only and stop under `prefers-reduced-motion`. WCAG 2.2.2: only the shop-type marquee loops forever, and it has a visible pause checkbox (works without JS); `tz-ping`, `tz-travel` and `tz-float` run one or two cycles and stop. `html { scroll-padding-top: 84px }` (Base) keeps focus and anchors clear of the sticky header.
- `src/lib/leads.ts`: typed `POST /v1/public/leads` with a camelCase body (`name`, `shopName`, `phone` in E.164 `+91XXXXXXXXXX`, `city`, `source`, `turnstileToken`); `TODO(W1-04)`: switch to `@todayz/api-client`. Only 400 and 422 say "we could not accept these details"; 429 and every other status (401, 403, 404, 405, 413, 415, 5xx) show the generic error. A request gives up after 15 s (`network`), and the form keeps the details. Each request carries an `Idempotency-Key` (a ULID): the same one on a retry of the same details, a new one when the details change or after a success (`LeadKeys`). `src/lib/env.ts` resolves `PUBLIC_API_URL` and `PUBLIC_TURNSTILE_SITE_KEY` (see Run), and `PUBLIC_CUSTOMER_APP_URL` / `PUBLIC_BUSINESS_APP_URL` (optional bare https origins, default `https://app.todayz.in` / `https://business.todayz.in`; `infra/local/local.env` points them at the local hosts).
- The lead form's submit button renders disabled and its script enables it; Turnstile's `data-language` is `en` or `hi`, else `auto`. An input carries `aria-describedby` only while its error shows. A missing check is written into the `role=status` region and focus moves to the widget (`tabindex=-1`) when nothing else is wrong. When `window.turnstile` is missing at submit, or the widget's `data-error-callback` (`tzTurnstileError`) fires, the form shows the generic error plus "the security check could not load" and sends nothing.
- The backend lets the site origins (`todayz.in`, `www.todayz.in`, locally `todayz.localhost`) call `/v1/public/*` only, with POST, `Content-Type` and `Idempotency-Key` and no credentials (D-102, `PublicCORSOrigins` in backend config).
- `src/lib/company.ts`: facts the pages show; anything undecided is null and the pages say so.
- `src/lib/seo.ts`: sitemap, robots and the old-URL redirect map; `public/_redirects` must equal `redirectsFile()` (tested).
- `public/og/`: committed Open Graph PNGs, one per page and language. Refresh with `pnpm build && pnpm og`.
- URLs end with a slash (`trailingSlash: 'always'`): page paths, links, canonicals, hreflang, `og:url` and the sitemap. check-site fails on an internal link without one.
- `scripts/headers.mjs` (after `astro build`): writes `dist/_headers` for Cloudflare Pages: a CSP (`script-src 'self' https://challenges.cloudflare.com`, Turnstile in `frame-src`, `connect-src` = the API origin read from the built forms, `frame-ancestors 'none'`), nosniff, Referrer-Policy, X-Frame-Options and Permissions-Policy; and `Cache-Control: public, max-age=31536000, immutable` on `/_astro/*`. `e2e/csp.spec.ts` serves the built pages with that CSP and fails on any `securitypolicyviolation`.
- `scripts/verify-dist.mjs` (`--e2e` in build:e2e): fails on any script except same-origin hashed `/_astro/*.js`, inline JSON-LD, and the Turnstile URL on pages that hold the lead form; on font CDNs, remote `url()`/`@import`, and remote `src`/`href`/`srcset` (only `https://todayz.in/` and `<a href>` to the app origins are allowed); on inline event handlers (`on*=`) and `javascript:` URLs; and, outside e2e, on a Turnstile test key or a local API origin. `scripts/check-site.mjs` then checks internal links, fragments, trailing slashes and the sitemap, and runs the HTML validator.
- `lighthouserc.json` + `scripts/lighthouse.mjs`: Lighthouse CI on dist/, mobile (default emulation) and desktop (preset), home in every language plus the other key pages; >= 95 in every category; filesystem output in `.lighthouseci/{mobile,desktop}/`; Turnstile host blocked. It runs at the end of `e2e:smoke`.
- Lighthouse exception (D-102): mobile performance on `/hi/` and `/te/` is gated at 0.85 (`assertMatrix` in `lighthouserc.json`); desktop and every other page and category stay at 0.95. Cause: the Indic display fonts from `@todayz/tokens` (Anek plus Noto Sans, full variable weight range: about 380KB for Devanagari and 265KB for Telugu, against 50 to 100KB for Kannada and Tamil) are large, and on throttled mobile they delay the largest paint. Follow-up after the exec demo: subset or weight-limit those fonts in `@todayz/tokens`, then remove the exception. The e2e build serves the draft locales (`/hi/`, `/kn/`, `/te/`, `/ta/`) with `noindex`, which fails Lighthouse's `is-crawlable`; on those pages the SEO category is replaced by its other audits (each must pass). Production builds do not contain draft locales.
- `e2e/`: `smoke.spec.ts`, `site.spec.ts` (SC-SITE-01..06; Turnstile and the leads API are stubbed in `helpers.ts`), `csp.spec.ts` (the built CSP), `screens.spec.ts` (only with `TZ_QA_DIR`).

## Conventions

- Always run Astro through the package scripts. They set `ASTRO_TELEMETRY_DISABLED` and `ASTRO_DISABLE_UPDATE_CHECK`, and pass `--ignore-lock` so dev and preview stay in the foreground. Astro 7 otherwise backgrounds itself when it detects an agent.
- Astro's prerender bundle inlines `cookie`, `html-escaper` and `zod` (`vite.environments.prerender.resolve.noExternal`). Under pnpm they are not resolvable from `dist/`.
- Styles live in `<style>` blocks and use tokens only (no raw hex). Stylelint checks `.astro` files; `src/lib/tokens.test.ts` checks every variable used exists in the tokens.
- All visible text goes through message keys. Never state a figure, price or claim the product rules do not hold. Product and shop names in mock screens stay English in every language.
- Test phone numbers come only from the reserved `+91 90000 000xx` block.

## Marketing exceptions to the design README

The landing pages follow `design/source` with these exceptions only:

- `tz-btn-brand` is allowed on the primary CTA (header "Get started", the lead form's submit).
- The idea band and the CTA card may use `--deep` as their background.
- More than one wash band per page is allowed.
- In the footer language list and the language switcher, a language name in a script other than the page's own (and other than English) uses the system font (`system-ui, sans-serif`). Otherwise every page downloads all four Noto Sans script fonts (about 380KB) for one word each, which fails Lighthouse mobile. Android's system Indic fonts are Noto, so the names look the same there.

Everything else in the design README still applies: status pills, stock dots, a freshness note on every stock figure ("In stock · updated 12 min ago"), no emoji. Mock shops, items and numbers are examples, and every landing page says so ("Shops, items and numbers on this page are examples.").

## Status

W1-09: all pages (S-01..S-06), lead form, SEO, localisation scaffolding, checks. Legal text is a draft awaiting legal review; non-English text is machine-drafted awaiting human review.
