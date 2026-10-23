# W1-08 App shells and sign-in UI: QA report

Branch `worktree-agent-a0a26af00032565dd`, rebased by the lead onto `main` at 7bd9b09. The agent could not write report files, so the lead wrote this from its two hand-backs and the lead's own runs. Nothing touched a cloud account or the network.

## What was built

- **`@todayz/api-client`:** the bearer is never sent to the sign-in routes (`otp/send`, `otp/verify`, `refresh`, `logout`, `logout-all`). Every `/v1/auth/*` call goes with `credentials: 'include'`, because the API is cross-origin and the refresh cookie must be stored.
- **`shared/auth`** (new package): the session core, the phone and OTP sign-in flow, shell pieces, and a build-time check of the site key.
  - **Token:** in memory only. Refresh is single-flight within a tab and serialised across tabs by a Web Lock, with a fallback when locks are missing or throw. It aborts after 15 s.
  - **Refresh timing:** the wait before a refresh has a 30 s floor. An invalid `expiresAt` falls back to a 5 min life, and expiry is corrected by the server's `Date` header.
  - **Cold start:**
    - A network failure or 5xx keeps the status `unknown` (offline) and retries on `online`, `visibilitychange` and a 5 s → 60 s backoff. Only a final 401 signs out.
    - A non-secret `todayz.hasSession.<app>` hint skips the start refresh for anonymous visitors.
  - **Sign-out:**
    - An epoch, bumped by every sign-out, drops refresh results that land afterwards. Cross-tab messages carry a stamp, so stale tokens and stale sign-outs are ignored.
    - An offline sign-out leaves a `todayz.logoutPending.<app>` hint, and the next load retries the logout before any refresh.
    - `signOutEverywhere()` refreshes once and retries once on a 401. On a second 401 it throws and stays signed in.
  - **401 handling:** decided by the problem code. `otp_expired` goes to the caller. Only `unauthorized`, or a 401 with no body, triggers refresh-and-retry, once.
  - **User switch:** `onUserChange` (decoding `sub` from the token, no verification) lets the apps clear their query cache, and the admin second-step flag, when another tab signs in as someone else.
  - **`safeNext` and `validateNextSearch`:** shared by all three apps. They reject control characters, backslashes, other origins and the gate routes.
  - **`authDefines`:** allows the always-pass Turnstile key and the local API URL only in modes `development`, `test` and `e2e`. Any other mode fails the build.
- **Customer:**
  - The app shell and sign-in UI (C-01), with the Resend error shown on the code step.
  - C-02 locality search is a stub (`TODO(W1-06)`).
- **Business:**
  - The sign-in gate, B-01 sign-in, and the shop picker.
  - The picker assumes `GET /v1/biz/businesses` returns `{items:[{id,name,role,stores:[{id,name}]}]}` (`TODO(W1-04)`).
- **Admin:**
  - The sign-in gate and the two-step page.
  - The TOTP calls match the W1-01 phase B contract (enrol, verify with `purpose: login|step_up`, recover, and `nextStep`), but are hand-typed (`TODO(W1-01b)`) until the generated client is swapped in.
  - The `otpauth://` link is rendered only when the URI has that scheme.
- **i18n:** sign-in catalogues for the three apps. hi, kn, te and ta are English copies for now.
- **Make:** `mk/46-auth.mk` adds `check-auth`, and `check-frontend-build` runs `build:e2e`.

## Evidence

| Check | Result |
|---|---|
| `pnpm install --frozen-lockfile --offline` with the hand-edited workspace importers in `pnpm-lock.yaml` | ok (lead) |
| typecheck, eslint and stylelint in customer, business, admin and shared/auth | clean (agent) |
| Unit tests | shared/auth 60, api-client `http.test` 21, customer 34, business 55, admin 40: all pass (agent) |
| `make check-auth check-frontend` in the worktree (with main's generated api-client copied in) | exit 0 (lead, after the copy fixes in ad8d7a3 and again after 6ae4973) |
| Initial JS, gzipped (first round) | customer 122.99 kB of 170, business 131.93 of 250, admin 123.53 of 250, all within budget (agent) |
| Copy check (`shared/i18n` sentence case) | passes. Product names `TodayZ Business` and `TodayZ Admin` were added as proper phrases, and quoted UI labels and screen names use curly quotes (D-103) (lead, ad8d7a3) |
| Independent security review of shared/auth, the gates and the token handling | 2 High, 7 Medium and 5 Low findings. Items 1–12 were fixed in 6ae4973. Items 13 (an extra rotation) and 14 (the `amr` routing hint) are left open, see below. |
| Re-review of 6ae4973 | 0 High, 6 Medium, 6 Low, all fixed in 96e4ce3 with a test each: sign-out forgets locally before the logout call and blocks refreshes during it; the hint reads "unknown" when storage is unavailable; token life from the JWT's `exp - iat` instead of the (unexposed) `Date` header; `lastUserId` survives sign-out so a different user clears the cache; `SessionPending` shows the offline notice and "Try again"; a 10 s logout timeout; dot-segment `next=`; monotonic stamps; signed-in retries; flush guard; WebOTP effect stability |
| Unit tests after 96e4ce3 | shared/auth 76, customer 36, business 56, admin 41, api-client 42: all pass (agent) |
| `make check-auth check-frontend` after 96e4ce3 | exit 0 (lead) |
| Stack journeys (Playwright, `playwright.stack.config.ts`) | run by the lead after merge; see "Stack run" below |

## Stack run

To be filled in after the merge and stack restart.

## Open items

- The admin "done" flag is a sessionStorage routing hint only. The server's `amr` check is what enforces access (review item 14).
- After a lock is released, a second tab can rotate the refresh token once more before the broadcast reaches it (review item 13). This is harmless: only one rotation is wasted.
- No step-up re-prompt flow, no TOTP QR code (the setup key and the `otpauth` link only), and no nav role filtering (W3-05).
- No screen calls `signOutEverywhere()` yet. The screen that does must show "did not complete" when it throws.
- If site data is cleared but the cookie survives, the device signs in again, because the hint is gone.
- The hi, kn, te and ta catalogues are English copies.
- The TOTP and logout-all calls are hand-typed until the generated client is wired in after W1-01b merges.

## Questions answered by the lead

- **Referral field on sign-up:** left out, because there is no contract yet (W2-03).
- **Cart surviving sign-in:** the cart is W2-09's; it must survive sign-in.
- **Hand-edited lockfile:** accepted, because the frozen offline install passes (D-88).

## Files outside the owned paths

- `mk/40-frontend.mk`, `mk/46-auth.mk` (new)
- `infra/local/local.env`
- `pnpm-lock.yaml` (workspace importers only)
- `shared/api-client` exports and `src/http.ts`
- `shared/i18n` catalogues and `test/i18n.test.ts` (the proper phrases)
- `shared/auth` (new), `shared/CLAUDE.md`, `customer/CLAUDE.md`
