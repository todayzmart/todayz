# MQ-07 Admin roles

Each of the seven admin roles signs in, checks that the sidebar shows exactly its screens, opens a hidden screen by URL and tries actions it must not take. Credit adjustments above ₹500 need a second person. A-17 shows provider keys only as set or missing.

- **Covers:** 08 §5 MQ-07; AD-01, AD-02, CR-15, CR-27, PO-03 (UI side), PL-05 (UI side), AU-11, AU-12, E2E-12.
- **Screens:** A-01 to A-20 (nav only for most; A-09, A-17, A-18 in depth).
- **Devices:** D1440 (profile D); a second window (profile E) for the second approver.
- **Time:** about 75 minutes.

## Preconditions

1. `make reset`; `clock reset`.
2. F41, S42, G43, Y44, FN45, FN46, AD47, SA48, SA49, D50 exist with the roles in README §6. AD47 is the single unenrolled seed admin; the others are already enrolled, and their TOTP codes come from the "TOTP now" panel in the mocks UI.
3. An authenticator app on a test phone (or `oathtool` with the secret shown at AD47's enrolment). Do not screenshot the QR, the secret or the recovery codes.
4. C18 (Lata) wallet 0.

### Expected sidebar per role (06 §3)

| Role | Account | Screens in the nav |
|---|---|---|
| `field_agent` | F41 | A-04 Create business, A-15 My stats, A-03 (Leads tab only) |
| `support` | S42 | A-02, A-03 (read-only), A-05, A-06, A-07, A-12, A-13, A-14, A-20 (9 screens) |
| `catalogue` | G43 | A-02, A-08 |
| `analyst` | Y44 | A-02 and a link to the Grafana dashboards |
| `finance` | FN45 | A-02, A-09, A-11 |
| `admin` | AD47 | A-02 to A-17, A-19, A-20 (no A-18) |
| `super_admin` | SA48 | A-02 to A-20 |
| `support` + `catalogue` | D50 | A-02, A-03 (read-only), A-05, A-06, A-07, A-08, A-12, A-13, A-14, A-20 (10 screens) |

## Steps

### A. Sign-in and TOTP enrolment (A-01)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.01 ★ | Open https://admin.todayz.localhost. Sign in as AD47 with OTP `000000`. | TOTP enrolment: a QR and a typed secret, then a field for the first code. No admin screen is reachable yet. | `R-2.5` AU-11 A-01 | | |
| MQ-07.02 | Open https://admin.todayz.localhost/orders in the same tab before finishing enrolment. | Back to the TOTP step. `GET /v1/admin/orders` with the current token → 401 or 403 (the token has `amr` = `otp`). | `R-2.5` AU-11 | | |
| MQ-07.03 | Enter a wrong TOTP code. | Refused with a translated message. | `R-2.5` A-01 | | |
| MQ-07.04 ★ | Enter the right code. | 10 recovery codes shown once, with a "I have saved them" confirm. After confirm, Pulse (A-02) opens. | `R-2.5` D-60 A-01 | | |
| MQ-07.05 | Reload the page. | The recovery codes are not shown again anywhere. | `R-2.5` | | |
| MQ-07.06 | Sign out. Sign in as AD47 again: OTP, then use one recovery code instead of TOTP. | Signed in. A-19 shows an audit row for the recovery code use. | `R-2.5` A-19 | | |
| MQ-07.07 | Sign out and try the same recovery code again. | Refused (one use only). | `R-2.5` | | |

### B. Sidebar per role (AD-02)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.09 ★ | Sign in as F41 (field agent). | Sidebar exactly as the table: Create business, My stats and Businesses showing only the Leads tab (list of site leads with a status control). No Pulse. | `R-13.2` AD-02 | ▢ | |
| MQ-07.10 | As F41 open `/pulse` by URL. | "You don't have access to this" empty state; no broken page; no 403 toast. `GET /v1/admin/pulse` → 403. | `R-13.2` AD-01 AD-02 | ▢ | |
| MQ-07.11 | As F41 open My stats. | Only F41's own row (0 shops onboarded). `GET /v1/admin/field/me/stats` returns F41's numbers only; `GET /v1/admin/field-agents` → 403. | `R-14.4` AD-01 A-15 | | |
| MQ-07.12 ★ | Sign in as S42 (support). | Sidebar exactly as the table (9 screens). A-03 opens read-only: no verify, request-info, reject, suspend, reinstate, reveal or document-review buttons, and `POST /v1/admin/businesses/<id>/verify` → 403. | `R-13.2` AD-02 | ▢ | |
| MQ-07.13 | As S42 open A-13 Risk and a flag. | Queue and evidence visible. No Clear or Confirm buttons. `POST /v1/admin/risk/flags/<id>/clear` → 403. | `R-13.2` A-13 | ▢ | |
| MQ-07.14 | As S42 open `/settings`, `/payouts`, `/credits`, `/users`, `/audit`, `/catalogue` by URL. | Each shows "You don't have access to this". | AD-02 | | |
| MQ-07.15 | As S42 open A-06 Customers, search C13 by exact phone. | Detail: orders, wallet, strikes (3). Reset strikes button visible. No Block button. | `R-13.2` A-06 | ▢ | |
| MQ-07.16 ★ | Sign in as G43 (catalogue). | Sidebar: Pulse and Catalogue only. | `R-13.2` AD-02 | ▢ | |
| MQ-07.17 | As G43 open the A-08 review queue → `<seed:pending-product>`. Merge it into the suggested duplicate. | Merged; audit row. | `R-13.2` AD-01 A-08 | ▢ | |
| MQ-07.18 | As G43 call `GET /v1/admin/orders`, `GET /v1/admin/payouts`, `PATCH /v1/admin/settings/order_accept_timeout_min`, `GET /v1/admin/risk/flags`. | 403 each. | AD-01 | | |
| MQ-07.19 ★ | Sign in as Y44 (analyst). | Sidebar: Pulse and the dashboards link only. Pulse shows numbers; no action buttons anywhere on Pulse. | `R-13.2` AD-01 AD-02 A-02 | ▢ | |
| MQ-07.20 | As Y44 call `POST /v1/admin/orders/<any open order>/cancel` with a reason, `POST /v1/admin/credit-adjustments`, `POST /v1/admin/pending-products/<id>/merge`. | 403 each. | AD-01 | | |
| MQ-07.21 ★ | Sign in as FN45 (finance). | Sidebar: Pulse, Credits, Payouts. | `R-13.2` AD-02 | ▢ | |
| MQ-07.22 | As FN45 open `/businesses` by URL; call `GET /v1/admin/businesses` and `POST /v1/admin/businesses/<id>/kyc/reveal`. | No access state; 403 and 403 (finance has no KYC access). | `R-13.2` D-64 | | |
| MQ-07.23 ★ | Sign in as AD47 (admin). | Every screen except Admin users (A-18). | `R-13.2` AD-02 | ▢ | |
| MQ-07.24 | As AD47 open `/users` by URL. | "You don't have access to this". | `R-13.2` A-18 | | |
| MQ-07.25 | As AD47 open A-09 Credits. | Ledger browser and expiry preview. No adjustment form. `POST /v1/admin/credit-adjustments` → 403. | `R-7.12` CR-27 A-09 | ▢ | |
| MQ-07.26 ★ | Sign in as SA48 (super_admin). | Every screen including A-18 Admin users. | `R-13.2` AD-02 | ▢ | |
| MQ-07.27 ★ | Sign in as D50 (support + catalogue). | Sidebar is the union: 10 screens as in the table. | `R-13.2` AD-02 | ▢ | |
| MQ-07.28 | Use each role's normal flows for 5 minutes (open lists, details). | No 403 toast or error appears in a normal flow (buttons the role cannot use are hidden, not refused). | AD-02 G1 | | |

### C. KYC, cancel, review and strike actions by role

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.29 | As AD47 open A-03 → Lakshmi Traders → KYC. Click Reveal on PAN. | Full PAN shown for this view; an audit row "kyc.reveal" with AD47. | `R-13.3` A-03 A-19 | ▢ | |
| MQ-07.30 | As S42 call the same reveal API. | 403. | `R-13.3` | | |
| MQ-07.31 | As S42 open A-07 → C16's placed order at SL → Cancel. Pick "Store request" from the admin-cancel reasons, leave "strike to" as none. | Reason list exactly: Customer request, Store request, Fraud suspected, Technical issue, Other. Order `cancelled`; no strike; audit row and timeline row with S42 and the reason. | `R-5.14` `R-5.34` BK-39 A-07 | ▢ | |
| MQ-07.32 | As FN45 call the admin cancel API on another open order. | 403. | `R-5.14` BK-39 | | |
| MQ-07.33 | As S42 open A-12 Reviews, hide a review with reason "QA test", then restore it with reason "QA restore". | Hidden, then restored; two audit rows with the reasons. | `R-10.3` A-12 | ▢ | |
| MQ-07.34 | As S42 open A-06 → C13 → Reset strikes, reason "QA". | Strikes 0; audit row. | `R-5.28` A-06 | | |
| MQ-07.35 | As S42 call `POST /v1/admin/customers/<C13 id>/block`. | 403 (block is admin and super_admin). | `R-13.2` | | |

### D. Two-person credit adjustment (A-09; CR-15, CR-27)

Amounts are credits (1 credit = ₹1), so ₹500.00 = 50000 paise = `admin_adjust_dual_approval_above_paise`.

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.36 ★ | As FN45 open A-09, account C18's wallet. Adjust +₹200, reason "QA +200". Pass step-up. | Posted at once. No pending request. C18 wallet 200. | `R-7.12` CR-15 A-09 | ▢ | |
| MQ-07.37 | Adjust +₹500.00, reason "QA +500". | Posted at once (at the threshold). Wallet 700. | `R-7.12` CR-15 | | |
| MQ-07.38 | Adjust −₹200, then −₹500.00. | Both posted at once. Wallet 0. | `R-7.12` CR-15 | | |
| MQ-07.39 ★ | Adjust +₹501, reason "QA +501". | Not posted. A pending request appears in Pending approvals with requester FN45, amount, reason and age. Wallet still 0. | `R-7.12` CR-15 A-09 | ▢ | |
| MQ-07.40 | Adjust −₹501, +₹600 and −₹600 the same way. | Three more pending requests; nothing posted. A fraction such as +₹500.50 is refused with `422 validation_failed`. | CR-15 | | |
| MQ-07.41 ★ | As FN45 open the +₹501 request. | Approve is disabled with "Needs a second person". Calling `POST /v1/admin/credit-adjustments/<id>/approve` as FN45 → 403 `approver_is_requester`. | `R-7.12` CR-27 `approver_is_requester` A-09 | ▢ | |
| MQ-07.42 ★ | In profile E sign in as FN46 and approve the +₹501 request with a note. | Posted; C18 wallet 501 (credits are whole numbers). Audit row holds both FN45 and FN46. | CR-15 CR-27 A-09 A-19 | ▢ | |
| MQ-07.43 | As FN46 reject the −₹501 request with note "QA reject". | Rejected; nothing posted; audit row. | CR-27 | | |
| MQ-07.44 | As SA48 request +₹600. | Pending (super_admin also needs a second person). | CR-27 | | |
| MQ-07.45 | As SA49 approve it. | Posted; audit holds SA48 and SA49. | CR-27 | | |
| MQ-07.46 | As AD47 and as S42 call the approve API on a pending request. | 403 each. | CR-27 | | |

### E. Settings and provider keys (A-17)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.47 ★ | As AD47 open A-17 Settings. | Keys grouped (orders, credits, referrals, reviews, limits, features, AI), each with description, unit (₹ for `_paise`, % for `_bp`, credits, minutes), min/max and history. Money keys (for example `credit_max_per_order`, `credit_min_order_value_paise`, `ref_customer_referee_credits`) are read-only for AD47. | `R-17.1` AD-01 A-17 | ▢ | |
| MQ-07.48 | As AD47 call `PATCH /v1/admin/settings/credit_max_per_order` with `{"value":40,"reason":"QA"}`. | 403. | `R-17.1` AD-01 | | |
| MQ-07.49 | `clock +11m`. As AD47 edit `order_accept_timeout_min` to 12 with reason "QA MQ-07.49". | Step-up TOTP prompt (last TOTP older than 10 min); without it the API returns `step_up_required`. After the code: saved; history row with old 10, new 12, AD47 and the reason. | `R-17.1` AU-12 `step_up_required` A-17 | ▢ | |
| MQ-07.50 | Set `order_accept_timeout_min` back to 10. | Saved; second history row. | `R-17.1` | | |
| MQ-07.51 ★ | As SA48 edit `credit_max_per_order` 30 → 40, reason "QA E2E-12", step-up. | Saved. In the customer app (C10, order ₹250 at SL) the credits slider now goes to 40, and the copy reads "Use up to 40 credits on orders of ₹100 or more". | `R-17.1` `R-7.3` E2E-12 G7 C-09 | ▢ | |
| MQ-07.52 | Set it back to 30. | Slider max 30 again on the next quote. | `R-17.1` | | |
| MQ-07.53 ★ | As AD47 scroll to Provider keys in A-17 (and A-16 integration status). | A read-only list of env var names (SMS, WhatsApp, push, Sentry, Anthropic, Sarvam) each showing `set` or `missing`. No value, no part of a value, no "show" control. The list matches `make env-check` for the same names. | `R-15.1` PL-05 D-19 A-16 A-17 | ▢ | |
| MQ-07.54 | Open DevTools → Network, reload A-17, search every response body for the first 6 characters of any key you know is set in `.env.local` (do not paste the value anywhere else). | No match. | PL-05 | | |
| MQ-07.55 | Turn `notify_whatsapp_enabled` on (key missing). | Saved. Integration status shows flag on, key `missing`, channel not used. Turn it off again. | `R-15.1` PL-05 A-16 | ▢ | |

### F. Admin users (A-18, super_admin only)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-07.56 | As SA48 open A-18. Invite `+91 90000 00032` with the role `analyst`. | Invited; audit row. Roles offered: exactly the 7 roles. | `R-13.2` A-18 | ▢ | |
| MQ-07.57 | Give the invited user a second role `catalogue`, then disable the user. | Roles saved; disabled user cannot sign in (sign-in refused). | `R-13.2` A-18 | | |
| MQ-07.58 | Reset TOTP for D50. | D50's next sign-in goes to TOTP enrolment again. | `R-2.5` A-18 | | |

## Cleanup

1. `restore defaults` (`order_accept_timeout_min` 10, `credit_max_per_order` 30, `notify_whatsapp_enabled` false).
2. `clock reset`.
3. `make reset`.
