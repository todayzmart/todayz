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
- Keep the original TodayZ primary logo artwork in the header and footer; never replace it with a letter, redraw it or hide it. `brand-accents.css` derives green-black glass tones from existing tokens for actions, the product-led discovery preview and the compact footer. Product listings use Lucide icons via `premium/product-icons.ts`; the original logo remains an image. `discovery-map.css` styles the illustrative street map, whose route follows its roads and keeps the existing replay hooks. Do not restore the decorative hero circle or giant footer wordmark.
- The lead form posts to `POST /v1/public/leads` (Turnstile protected).

## Layout (W1-09)

- `astro.config.mjs`: static output, dev port 5304 (strictPort), Host `todayz.localhost`, dev toolbar off. A small integration serves and copies the `@todayz/tokens` icons and exposes the favicon and theme-color tags as `virtual:todayz-head`.
- `src/lib/pages.ts`: the page registry (id, English path, locales, title and description keys, lead-form flag). Routes (`src/pages/[...slug].astro`), sitemap, hreflang, footer and tests all read it. Home and retailers exist in en, hi, kn, te and ta; every other page is English only.
- `src/i18n/`: loads the site's catalogues from `@todayz/i18n` (`shared/i18n/messages/site/<area>.<lc>.json`, owned by W1-09, checked by `make check-i18n-messages`). English files hold `{defaultMessage}` descriptors; translations hold `{message, review, enHash}`. Only literals and `{name}` arguments. A locale ships only when every key of `common`, `home` and `retailers` is `reviewed` and current (`isMachineDraft` fails closed); production builds (every mode except `development` and `e2e`) leave draft locales out entirely: no pages, sitemap entries, hreflang, switcher or footer links, OG images (`siteLocales`). The site does not import `@todayz/ui`.
- `src/layouts/Base.astro`: head tags (canonical, hreflang, Open Graph, Twitter, JSON-LD), skip link, header, machine-draft banner, footer. The Turnstile script is added only when the page holds the lead form.
- `src/views/`: one view per page kind (Home, Retailers, Info, Legal, NotFound). `src/components/`: Header (sticky glass), Footer (compact dark glass with app entry points), LangSwitch, Faq, Section, Steps, Icon (Lucide, inline SVG), LeadForm. `src/components/home/`: the landing sections ported from the landing prototype (hero scene, marquee, problems, bento, showcase, demand insights, shoppers, shop types, three steps, idea band, testimonials, FAQ, CTA); `RetailersView` reuses them. `Testimonials` renders nothing while it has no real quotes.
- Bright look (D-260): `Base.astro` sets `<body data-skin="bright">` (the `[data-skin="bright"]` rules in `@todayz/tokens`) on `--page`; cards are white with `--shadow-card` and no outline. The shop types are pastel tiles (D-269, `.bp .b-types` in `bright.css`, so `ShopTypes.astro` wraps them in a `.bp`) with a cut-out CC0 or public-domain photo multiplied onto the type's tint, from `src/assets/photos/` (sources in `SOURCES.md`), imported in `components/home/data.ts`; `e2e/screens.spec.ts` loads lazy images before its full-page shots.
- Bright landing (D-262): `src/components/bright/` holds the sections ported from `design/trials/site-home-bright` (Hero with dark text on the mint wash and no photo (D-267), SearchStrip, Shelf, radar, Cost, Week, savings calculator, Owners, PhotoCredits, and PageHead, the light head of every inner page), with example data in `bright/data.ts` and behaviour in `bright/bright.ts` (one same-origin script). `src/styles/bright.css` styles them, every rule under `.bp`. Home is all bright. For retailers has the bright hero and owners block, then the `.lp` sections. Info, legal and 404 use PageHead. Photos live in `src/assets/landing/`, with sources in `CREDITS.md`; the product photos are CC BY 2.0, credited on the page, and to be replaced before launch. The page's claims follow the product rules: per-shop cashback 0 to 20%, no pickup fee, 30 credits per referral, a 2 km default radius, and "confirmed N min ago" on every stock figure.
- `src/styles/landing.css`: the older landing styles, every rule scoped under `.lp`. Mock screens are `aria-hidden` and their inline `height`/`width` percentages are the only inline styles. Animations are CSS only and stop under `prefers-reduced-motion`. WCAG 2.2.2: only the bright search strip's two rows loop forever, and they have a visible pause checkbox (works without JS); their looping copy is `aria-hidden` with `tabindex=-1` links. Every other animation runs a few cycles and stops. `html { scroll-padding-top: 84px }` (Base) keeps focus and anchors clear of the sticky header.
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
- Lighthouse exception (D-102, widened to `/kn/` by D-262): mobile performance on `/hi/`, `/kn/` and `/te/` is gated at 0.85 (`assertMatrix` in `lighthouserc.json`); desktop and every other page and category stay at 0.95. Cause: the Indic display fonts from `@todayz/tokens` (Anek plus Noto Sans, full variable weight range: about 380KB for Devanagari and 265KB for Telugu, against 50 to 100KB for Kannada and Tamil) are large, and on throttled mobile they delay the largest paint. On `/kn/` the hero waited about 2.8 s for the Kannada fonts (0.92 on two runs; English home 0.98); since D-267 the largest paint is the hero title itself. Follow-up: subset or preload those fonts in `@todayz/tokens`, then remove the exception for all three. The e2e build serves the draft locales (`/hi/`, `/kn/`, `/te/`, `/ta/`) with `noindex`, which fails Lighthouse's `is-crawlable`; on those pages the SEO category is replaced by its other audits (each must pass). Production builds do not contain draft locales.
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

W1-09: all pages (S-01..S-06), lead form, SEO, localisation scaffolding, checks. Legal text is a draft awaiting legal review. The founder approved the hi, kn, te and ta text as it stands (D-262), so all five languages are reviewed and ship.

## Liquid Silver presentation — 30 Oct 2026

As built on base commit e986a157, with this presentation change. The founder requested the customer app's selected Liquid Silver direction on todayz.in. Base.astro loads styles/silver.css on every route: silver page and neutral semantic tokens, graphite actions, frosted header and search, translucent cards, and silver image wells. Original product photos and semantic status/category colours remain available. Home's existing scroll reveals use a slower easing curve; hero elements enter in sequence, the hero light drifts once, and cards and photos respond to hover. All new motion respects reduced motion. No additional client script or dependency is added. Locale typography, content, forms, navigation and SEO use the existing implementation.

The stylesheet uses the opt-in silver-* shared tokens introduced by the customer design. Deployment uses the existing main-branch Cloudflare Pages workflow; automatic CI remains paused as requested separately. Static token checks and whitespace validation are performed locally; production compilation is verified by the deployment workflow. Browser visual review remains pending.

## Editorial UX rebuild — 31 Oct 2026

The founder requested a structural redesign beyond palette changes. HomeView now starts with a split editorial introduction, a query-entry search and a product-preview panel built from the existing example catalogue. Search navigates to the configured customer app /search?q= URL; a no-script link preserves access, and the CSP stays intact. A proof strip and sticky chapter navigation lead into discovery, journey, nearby-shop exploration, cost comparison, weekly benefits, savings, retailer signup and FAQ. Every existing feature component, endpoint, lead-form source, translation and SEO input is retained. The shared frame has more spacious navigation, editorial page headings and a revised footer/signup layout. styles/editorial.css owns responsive structure and motion. Product examples retain freshness labels and the example notice. Motion respects the global reduced-motion policy.

Validation: all eleven homepage feature components remain present, CSS token references resolve and whitespace checks pass. Production build is delegated to the existing deployment workflow. Browser visual review remains pending.


## Independent premium homepage — 31 Oct 2026

The homepage now imports `premium.css` and `components/premium`, without bright.css or landing.css. It replaces the ticker and floating secondary navigation with a finite category directory, replaces the radar with a keyboard-operable shop list/detail explorer, and rebuilds product presentation, comparison disclosures, journey layout, calculator, FAQ and lead-form layout. Shared navigation uses a compact wordmark frame; the announcement bar is removed. Existing translations, example data, walking replay, filtering, savings math, lead endpoint and app URLs remain. Motion is bounded and respects reduced motion. The original bright components remain for other routes. Production build and live review are required for this change.
