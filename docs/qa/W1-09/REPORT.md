# W1-09 Marketing site: QA report

Astro site in `site/`, served locally at https://todayz.localhost. Branch `worktree-agent-ac76c623d97c2e90d`, rebased by the lead onto `main` f4aa7f1 and merged on 2026-10-23. Everything ran locally; nothing touched a cloud account.

## What was built

- **Pages:** home (the landing redesign ported from the lead's prototype), for retailers, how it works, pricing, about, contact, 404, and the legal set (privacy, terms for shoppers, terms for shops, refunds and credits).
- **Locales:** en, plus hi, kn, te and ta as machine drafts. Drafts appear only in local and e2e builds, with a banner and `noindex`; production builds leave them out entirely (D-102).
- **Lead form:** posts to `/v1/public/leads` with an `Idempotency-Key`, a 15 s timeout and Turnstile. Only 400 and 422 count as "details rejected"; other failures offer a retry. The payload is typed locally until W1-04 adds the contract (`TODO(W1-04)`).
- **SEO:** sitemap, robots, redirects, JSON-LD and OG images.
- **Build guards:** `verify-dist` keeps an allow-list (own hashed bundles and Turnstile only, no remote assets or inline handlers). There are link and HTML checks, a Playwright CSP test, and `Cache-Control` on `/_astro`.
- **Motion:** the marquee has a pause control that works without JS. The float, ping and travel animations stop after a few cycles, and reduced motion is honoured.
- **Backend (D-102):** a separate, uncredentialed CORS rule for `/v1/public/*`:
  - site origins only;
  - POST and OPTIONS;
  - headers `Content-Type` and `Idempotency-Key`;
  - the path is cleaned first, so `/v1/public/../auth/refresh` is not public.

  The shared credentialed list is unchanged (D-14).

## Evidence

The agent ran these after the rebase onto f4aa7f1; the lead reran the first five.

| Check | Result |
|---|---|
| `pnpm install --frozen-lockfile --offline` | exit 0. Lockfile subset check: 0 packages not in either side; a second offline settle left the file byte-identical |
| Site lint, CSS lint, typecheck | clean |
| Site unit tests | 172 passed |
| `build:e2e` | dist ok (61 files), links and HTML valid |
| `@todayz/i18n` tests and message lint | 197 passed; 0 errors in every app (warnings are the unreviewed hi/kn/te/ta drafts) |
| `go test -race` config and httpx (8 packages) | ok, including the tests that refuse a site preflight to `/v1/auth/refresh`, `/v1/public/../auth/refresh` and `/v1/publicity` |
| golangci-lint | 0 issues (agent) |
| `e2e:smoke` | 68 Playwright tests passed (agent); the 32 skipped are the screenshot spec, which runs only with `TZ_QA_DIR` |
| CSP test sensitivity | fails when `challenges.cloudflare.com` is removed from the policy (agent) |

**Lighthouse (agent's latest run).** Columns are performance, accessibility, best practices and SEO, on mobile.

| Page | Scores |
|---|---|
| `/` | 1.00, 1.00, 1.00, 1.00 |
| `/hi/` | 0.89, 1.00, 1.00, 0.66 |
| `/kn/` | 0.96, 1.00, 1.00, 0.66 |
| `/te/` | 0.91, 1.00, 1.00, 0.66 |
| `/ta/` | 0.99, 0.96, 1.00, 0.66 |
| `/legal/privacy/` | 0.96, 1.00, 1.00, 1.00 |
| Every other page | 1.00 in every category |

- **Desktop:** 1.00 for performance, accessibility and best practices on every page.
- **Draft-locale SEO (0.66):** the score is low only because drafts are served with `noindex`. For those pages the assertions check each other SEO audit instead.
- **Mobile performance gate:**
  - 0.85 on `/hi/` and `/te/` until the Indic fonts are subset (D-102);
  - 0.95 everywhere else.
  - `/kn/` sits at 0.95–0.96 and could flake.

## Screenshots

`docs/qa/W1-09/screens/`: every page at 390 and 1440 px, home in each draft locale, reduced motion.

## Lead changes at merge

- Recorded D-103: the site catalogue lives in `shared/i18n/messages/site/`, and D-102 is amended to match.
- The sentence-case check now ignores words inside curly quotes (“Open now”), which fixes the failing `@todayz/i18n` test on main.

## Open

1. **For W1-04:** leave `turnstileToken` out of the idempotency hash for `/v1/public/leads` (D-103).
2. **Staging origin:** `https://staging.todayz.in` in `PublicCORSOrigins` is a placeholder until Wave 5.
3. **Indic fonts:** subset the Indic fonts in `@todayz/tokens`, then restore the 0.95 gate on `/hi/` and `/te/`.
4. **Founder questions:**
   - grievance officer details;
   - legal sign-off on the policy pages;
   - the real Turnstile site key and API URL;
   - the old site's URL list for redirects.
5. **Translations:** native review of the hi, kn, te and ta site copy before any of them ships.

## Not verified

- Real Turnstile and a real API; both are stubbed in e2e, and the backend lead route does not exist yet (W1-04).
