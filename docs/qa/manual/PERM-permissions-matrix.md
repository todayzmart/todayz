# PERM Permissions matrix

Every business role and every admin role against the actions that matter, in the UI and by direct API call. The UI must hide what a role cannot do (a user never meets a 403 in a normal flow); the API must refuse it anyway. MQ-07 covers the admin sign-in and the main per-role flows; this file fills the rest of the matrix.

- **Covers:** AU-09, AU-10, BZ-01, PO-03, AD-01, AD-02, FA-01; D-06, D-38, D-39, D-64, D-65, D-66; R-2.8, R-3.1, R-3.6 to R-3.11.
- **Screens:** B-01, B-07, B-09, B-10, B-13, B-14; A-02 to A-20 (nav only).
- **Devices:** D1280 with one profile per person; D1440 for admin.
- **Time:** about 2 hours.

## Preconditions

1. `make reset`. Seeds `<seed:staff>`, `<seed:admins>` (TOTP enrolled as in MQ-07.08), `<seed:month-SL>`.
2. For each API row: do the action once as an allowed user with DevTools open, Copy as cURL, then swap in the other user's bearer token from their own session (README §5). Record status and `code` only.
3. Fill the result grid in the run notes: rows are actions, columns are roles, each cell the status you saw.

## Steps

### A. One phone, many profiles

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.01 | Sign in to the customer app as X25 (`+91 90000 00025`). Then, in the same browser, sign in to the business app as X25. | Both sign-ins succeed with the same phone. The customer profile has its own wallet and orders; the business app shows the B-01 picker with Annapurna Provisions (owner) and Lakshmi Traders (cashier). | `R-2.8` `R-2.4` B-01 | ▢ | |
| PERM-M.02 | Sign out of the customer app only. Reload the business app. | The business app stays signed in (cookies are per app, D-14). | `R-2.7` AU-01 AU-13 | | |
| PERM-M.03 | In the business app pick Lakshmi Traders. Then switch to Annapurna Provisions. | At Lakshmi Traders the nav is the cashier set (no B-09, B-13, B-10 credit account, offers). At Annapurna the full owner set. | `R-2.4` `R-3.9` B-01 | ▢ | |

### B. Store scope and the business boundary

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.04 ★ | As K22 (Sri Lakshmi only) call `GET {S}/items` for Sri Lakshmi 2. | `403`. The store picker in the app shows Sri Lakshmi only. | `R-3.1` AU-09 | | |
| PERM-M.05 | As M24 (manager, Sri Lakshmi 2 only) open B-07 and B-04. Call `GET {S}/orders` for Sri Lakshmi. | Only Sri Lakshmi 2 data. API `403` for Sri Lakshmi. | `R-3.1` `R-3.8` | | |
| PERM-M.06 | As O26 (owner of Kaveri Fresh) call `GET /v1/biz/businesses/{Lakshmi Traders}/stores/{SL}/items` and `GET {B}/staff` for Lakshmi Traders. | `403` each: a business token for business A on business B. | AU-09 | | |
| PERM-M.07 | As C10 (customer token) call `GET {S}/orders` for Sri Lakshmi. As K22 call `GET /v1/me/orders`. | `403` on the business endpoint. The customer endpoint for K22 returns K22's own customer profile orders (empty) or `403`; record which. | AU-09 | | |

### C. Staff management (who manages whom)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.08 ★ | As O20 open B-13. Invite `+91 90000 00030` as manager for Sri Lakshmi, then `+91 90000 00031` as cashier. Change the cashier to manager. Remove both. | Each step succeeds. Audit rows show O20. | `R-3.7` BZ-01 B-13 | ▢ | |
| PERM-M.09 ★ | As M21 open B-13 → Invite. | The role field offers cashier only. Invite `+91 90000 00031` as cashier → added. Remove shows on cashier rows only; manager and owner rows are read-only. | `R-3.8` `R-3.10` BZ-01 D-66 B-13 | ▢ | |
| PERM-M.10 | As M21 via API: `POST {B}/staff` with role `manager`; `DELETE {B}/staff/{M24}`; `DELETE {B}/staff/{O20}`; `PATCH {B}/staff/{K22}` with role `manager`. | `403` for each. | BZ-01 D-66 | | |
| PERM-M.11 | As K22 open `/staff` by URL and call `GET {B}/staff` and `POST {B}/staff`. | No B-13 in the nav; the URL shows the no-access state. API `403` for both. | `R-3.9` BZ-01 | | |
| PERM-M.12 | As O20 open B-13 and look for a delete or close business action. Call `DELETE {B}`. | Only "To close your business, contact support" with a link to B-15. API `405` (no such endpoint; record the actual status). | `R-3.11` BZ-01 D-66 B-13 | ▢ | |

### D. Payout details, KYC and money screens

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.13 | As O20 open B-14 → Payout details. As M21 open the same. | O20 sees edit fields. M21 sees PAN, GSTIN and UPI masked to the last 4 characters, legal name and status, and no Edit. | `R-3.6` `R-3.8` D-38 D-44 B-14 | ▢ | |
| PERM-M.14 | As M21 call `PUT {B}/kyc` with a new UPI. As K22 call `GET {B}/kyc`. | `403` for both. M21's `GET {B}/kyc` returns masked values only (no full PAN in the JSON). | `R-3.6` AU-10 | | |
| PERM-M.15 | As K22 call `GET {B}/credit-account`, `GET {B}/statements`, `GET {S}/analytics?range=7d`, `GET {S}/customers`. | `403` each. | AU-10 `R-3.9` | | |
| PERM-M.16 | As M21 open B-09 Customers at Sri Lakshmi. | Repeat customers with name, masked phone (`98•••••521` style), visits, spend, last visit, including a phone-only POS customer. No messaging action. | `R-3.10` `R-16.2` B-09 D-07 | ▢ | |
| PERM-M.17 | As K22 open `/customers` by URL. | Not in K22's nav; no-access state. | `R-3.10` B-09 | | |

### E. Cashier and manager actions (the full D-65 list)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.18 | As K22, for one order each: accept, reject, partial accept, mark ready, dispatch (R23), mark delivered, mark delivery failed, enter the code. | Each allowed (2xx). | AU-10 D-65 | | |
| PERM-M.19 | As K22 call cancel on an accepted order, `POST {S}/pos/sales/{id}/reverse`, and a POS sale with a manual line discount. | `403` each. The POS has no discount control for K22 and no Reverse on sales. Automatic round-off to ₹1 still applies. | AU-10 D-06 D-65 | | |
| PERM-M.20 | As K22 open B-06 Voice and record a draft with a `restock` line. | The line shows disabled with "Ask a manager"; `sale` and `count` lines can be confirmed. | D-65 B-06 VO-01 | ▢ | |
| PERM-M.21 | As M21 do each of: restock, damage, price edit, batch, import, offer, banner, review reply, coach card Done, POS reverse within 24 h, cancel an accepted order with `customer_unreachable`. | Each allowed. | `R-3.8` | | |

### F. Admin roles: nav and API

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.22 ★ | Sign in as each of F40, S42, G43, Y44, FN45, AD47 and SA48 in turn. Write down the sidebar items. | Exactly the 06 §3 table: F40 A-04 and A-15 (own row); S42 A-02, A-05, A-06, A-07, A-12, A-13, A-14, A-20; G43 A-02, A-08; Y44 A-02; FN45 A-02, A-09, A-11; AD47 every screen except A-18; SA48 everything. D50 sees the union of support and catalogue. | AD-02 D-64 | ▢ | |
| PERM-M.23 | As each role, open three hidden screens by URL. | "You don't have access to this" every time; no broken page, no error toast. | AD-02 | ▢ | |
| PERM-M.24 | Payout actions on a pending payout as FN45, AD47, S42, G43, Y44 and F40: hold, unhold, mark paid (UTR + step-up), export. | FN45 and SA48: allowed. AD47: hold and mark paid `403` (A-11 shows the list without those buttons). S42, G43, Y44, F40: `403` on every payout action. FN45 sees bank details masked to the last 4. | PO-03 D-64 A-11 | | |
| PERM-M.25 | As AD47 call `POST /v1/admin/credit-adjustments` (+₹10). As SA48 do the same. | AD47 `403` (A-09 form hidden). SA48 posts (under ₹500). | AD-01 D-64 A-09 | | |
| PERM-M.26 | As AD47 `PATCH /v1/admin/settings/search_w_offer` to 600 (unit `bp`, so a money setting by the unit rule; Q-45), and `PATCH /v1/admin/settings/stock_stale_after_hours` to 72 with reason "QA". | Money key `403`. Non-money key allowed (step-up). A-17 shows money keys read-only for AD47. | `R-17.1` D-64 A-17 | | |
| PERM-M.27 | As AD47 and as S42 call `POST /v1/admin/businesses/{AP}/verify`. As FN45 call `GET /v1/admin/businesses/{AP}`. | AD47 allowed (or `409 invalid_transition` if already verified; use SB's business). S42 `403` (support cannot verify KYC). FN45 `403` (finance has no KYC). | AD-01 D-64 A-03 | | |

### G. Field agent edit window

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| PERM-M.28 ★ | F40 onboarded Annapurna Provisions, verified 10 days ago. As F40 add an item to Annapurna on A-04 (`POST /v1/admin/field/businesses/{AP}/stores/{sid}/items`). | Allowed. | `R-14.3` FA-01 D-39 A-04 | | |
| PERM-M.29 | `clock` to verification + 29 days 23 h. F40 edits that item's price. | Allowed. | FA-01 | | |
| PERM-M.30 | `clock` to verification + 30 days + 1 s (`field_agent_edit_window_days` 30). F40 edits again. | `403`. A-04 shows the items read-only. | `R-14.3` FA-01 | ▢ | |
| PERM-M.31 | As F41 (onboarded nothing) call the same items endpoint for Annapurna, and `GET /v1/admin/field/businesses`. | `403` on Annapurna. The list is empty. | FA-01 | | |
| PERM-M.32 | As F40 open A-15. As AD47 open A-15. | F40 sees only their own row and trend (`/v1/admin/field/me/stats`). AD47 sees every agent: onboarded, verified, live, products added, first-order rate. | `R-14.4` A-15 D-64 | ▢ | |

## Cleanup

1. Remove invited staff `+91 90000 00030`, `…31`.
2. `clock reset`; `make reset`.
