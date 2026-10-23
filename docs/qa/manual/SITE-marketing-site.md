# SITE Marketing site

The Astro site at https://todayz.localhost (todayz.in in production): pages S-01 to S-06, zero client JS, the lead form, localised pages and the SEO basics. Static HTML, light theme, design tokens only.

- **Covers:** SC-SITE-01…06; S-01 to S-06; R-1.1, R-1.2, R-2.6; D-58.
- **Screens:** S-01, S-02, S-03, S-04, S-05, S-06.
- **Devices:** D1440, P360; Lighthouse in Chrome DevTools; a link checker (`npx linkinator` or the browser).
- **Time:** about 75 minutes.

## Preconditions

1. `make dev`. https://todayz.localhost opens. Mocks UI open (Turnstile tab).
2. Build the static output once: `pnpm --filter @todayz/site build`. Keep `site/dist` for M.03.

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| SITE-M.01 ★ | Open S-01 Home at D1440 and P360. | Real HTML (no iframe). Sections: hero for shops and shoppers, how it works, Demand insights showcase, for shoppers with app link, testimonials placeholder, FAQ, CTA "Get your shop on TodayZ" that goes to the lead form. White page, mint wash, Anek headings and Figtree body, Lucide icons. | S-01 | ▢ | |
| SITE-M.02 ★ | DevTools → Network → JS filter, reload S-01, S-02 and S-04. Also view source. | No script requests and no `<script>` tag (zero JS). No request to a font CDN or any host other than todayz.localhost. | S-01 SC-SITE | ▢ | |
| SITE-M.03 | Run `node site/scripts/verify-dist.mjs` (or the package's verify script) on the build. | Passes: no client JS, no remote `src`/`href`, no font CDN. | SC-SITE | | |
| SITE-M.04 | Open S-02 For retailers. | Benefits: free POS, online orders, demand insights, credits bring customers back. Steps, FAQ, lead form. | S-02 | ▢ | |
| SITE-M.05 | Open S-03 How it works, Pricing, About and Contact. | Pricing says "Free for now". Each page has a unique `<title>` and `<h1>`. | S-03 `R-1.3` | ▢ | |
| SITE-M.06 | Open each S-04 Legal page: Privacy, Terms for customers, Terms for retailers, Refund/cancellation. | Privacy names DPDP basics and a grievance officer. Refund/cancellation explains the credits rules (1 credit = ₹1, not cash; record the text against 01 §7). | S-04 `R-7.1` | ▢ | |
| SITE-M.07 ★ | Fill the lead form on S-02: name "QA Shop", phone `+91 90000 00140`, shop "QA Kirana", city "Bengaluru". Submit. | A thank-you state. Network: `POST https://api.todayz.localhost/v1/public/leads` → 2xx. Turnstile tab shows one verify with success. | S-02 `R-2.6` W1-09 | ▢ | |
| SITE-M.08 | Submit again with the Turnstile token set to `XXXX.DUMMY.TOKEN.FAIL` (DevTools: edit and resend the request). | Refused (record status and `code`); no lead stored; the Turnstile tab shows `invalid-input-response`. | `R-2.6` | | |
| SITE-M.09 | Submit with an empty phone, then a 9-digit phone. | Field errors next to the field; no request sent for the empty phone. Zero JS means validation is HTML (`required`, `pattern`) or server-side; record which. The Turnstile script (`challenges.cloudflare.com/turnstile/v0/api.js`) is the one allowed script, on the lead-form page only (D-93). | S-02 | | |
| SITE-M.10 | Find where the lead lands. | It appears in the Leads tab on A-03 (field agent, support, admin, super_admin) with status `new`. | W1-09 | | |
| SITE-M.11 | Open S-05 home and retailers in hi, kn, te and ta (`/hi/`, `/kn/`, `/te/`, `/ta/` or the built paths). View source. | Translated text in each script. `<html lang>` matches. Each page has `hreflang` links to all five versions plus `x-default`. | S-05 `R-1.2` | ▢ | |
| SITE-M.12 | Open `/sitemap.xml` and `/robots.txt`. | Sitemap lists every page and language version with absolute `https://todayz.in/` URLs. Robots allows crawling and points to the sitemap. | S-06 | ▢ | |
| SITE-M.13 | View source on S-01, S-02 and S-04 Privacy. | One `rel=canonical` each (`https://todayz.in/...`); `og:title`, `og:description`, `og:image` per page; JSON-LD `Organization` (and `WebSite`) that parses in a JSON-LD validator. | S-06 | | |
| SITE-M.14 | Open three old todayz.in paths from the redirect map in `site/` (for example `/about-us` if listed). | `301` to the new page, one hop. If the redirect map is not in the repo yet, mark **B** (09 §5 expects it before cut-over). | S-06 | | |
| SITE-M.15 | Run the link checker over the built site. | No broken internal links. External links (app store, app link) point to `app.todayz.in` / `business.todayz.in`. | SC-SITE | | |
| SITE-M.16 ★ | Lighthouse (mobile) on S-01, S-02 and the Hindi home. | ≥ 95 in Performance, Accessibility, Best Practices and SEO on each. Reports saved under `runs/<date>/` only (D-58). | S-01 D-58 | ▢ | |
| SITE-M.17 | Open `https://todayz.localhost/_health`. | The local stack health page renders (local only). | README §1 | | |
| SITE-M.18 | Turn on dark mode in the OS and reload S-01. | Stays light theme (light only). | `R-1.2` | | |

## Cleanup

1. Delete the QA lead if an admin screen allows it; otherwise `make reset`.
