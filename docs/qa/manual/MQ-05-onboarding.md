# MQ-05 ★ Onboarding

A shop owner onboards on their own, gets sent back for one fix, is verified and goes live. A field agent onboards a second shop on the owner's behalf, the owner confirms by OTP, and the agent's edit window closes. An owner adds a new branch that needs a store review.

- **Covers:** 08 §5 MQ-05 (self-serve wizard and field agent flow with owner OTP); E2E-08; FA-01 manual side; SR-01 for new stores; SC-BZ onboarding items.
- **Screens:** B-01, B-02, B-03, B-10, B-13, B-14; A-03, A-04, A-05, A-15; C-03, C-05, C-07.
- **Devices:** owner phone P360 (profile C); admin D1440 (profile D); field agent phone P390 (profile E); customer P390 (profile A).
- **Time:** about 70 minutes.

## Preconditions

1. `make reset`; `clock reset`.
2. N30, N31, N32 have no account. F40 (Rahul) and F41 (Priya) are field agents; AD47 is `admin`.
3. The shop location used below is "12th Main, HAL 2nd Stage, 560038", about 1.3 km from the QA point. The category is Grocery.
4. Have one JPEG under 2 MB for storefront and shop photos.

## Steps

### A. Self-serve wizard (profile C, N30)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.01 ★ | Open https://business.todayz.localhost, sign in as N30 (OTP `000000`). | New user: no picker; the onboarding wizard (B-02) starts at Language. | `R-2.4` B-02 | ▢ | |
| MQ-05.02 | Pick English. Business step: brand "Ganesh Stores", category Grocery. Next. | Moves to Store. A step indicator shows progress. | `R-3.2` B-02 | | |
| MQ-05.03 | Store step: name "Ganesh Stores HAL", address as in precondition 3, drop the pin, hours 08:00–21:00 every day. Next. | Moves to Contact. | `R-3.2` B-02 | ▢ | |
| MQ-05.04 | Close the browser tab. Reopen the business app. | Signed in still; the wizard resumes at Contact with the earlier answers kept (save and resume). | `R-3.2` SC-BZ B-02 | ▢ | |
| MQ-05.05 | Contact: leave the contact name empty, tap Next, then go straight to Submit. | Submit is refused with a list of missing fields that names "Contact person". Nothing is submitted. | `R-3.2` SC-BZ B-02 | ▢ | |
| MQ-05.06 | Fill contact "Ganesh Rao" and mobile `+91 90000 00030`. Next. | Starter catalogue step. | `R-3.2` B-02 | | |
| MQ-05.07 ★ | Starter catalogue: pick category Dairy, choose Amul Butter 100 g and Nandini Toned Milk 500 ml. | Prices prefill with the MRP (₹60 and ₹24). Change Amul Butter to ₹57. | `R-4.1` B-02 | ▢ | |
| MQ-05.08 | Set opening stock 12 for Amul Butter and 20 for milk. Next. | Saved. (Opening stock writes `opening` movements: checked in MQ-05.23.) | `R-4.4` B-02 | | |
| MQ-05.09 ★ | QR standee preview step. | A preview of the A5 standee: shop name, QR, text in English, and the credit number read from settings ("100 credits" for a new customer). | `R-3.12` ST-01 B-02 | ▢ | |
| MQ-05.10 ★ | Submit. | Business status `submitted`. A status banner "We're checking your details" (or similar) stays at the top until verified. | `R-3.3` B-02 | ▢ | |
| MQ-05.11 | In profile A (QA point, 2 km) search "Ganesh". | Not found. The store does not show on Home (business not verified). | `R-3.4` SR-01 | | |

### B. Payout details and KYC formats (profile C)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.12 | B-14 → Payout details. Type PAN `ABCDE1234` (9 chars). | Format error; not saved. | `R-3.6` SC-BZ B-14 | | |
| MQ-05.13 | Type PAN `ABCPE1234F` (a synthetic, valid-format PAN), GSTIN `29ABCPE1234F1Z0` (bad checksum). | GSTIN refused for its checksum. | `R-3.6` SC-BZ B-14 | ▢ | |
| MQ-05.14 | Leave GSTIN empty (not registered). UPI `ganesh.qa@upi`, legal name "Ganesh Rao". Save. | Saved. The screen shows PAN `••••••234F`, UPI `•••••••••@upi` style masked to the last 4 characters. UPI status "Pending admin check". | `R-3.6` D-44 B-14 | ▢ | |

### C. Admin review: needs info, then verified (profile D, AD47)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.15 ★ | Sign in to https://admin.todayz.localhost as AD47 (OTP, then TOTP; enrol first if asked). Open A-03 Businesses → Submitted. | "Ganesh Stores" is in the queue with submitted time. | `R-13.1` A-03 | ▢ | |
| MQ-05.16 | Open it. | Profile, stores (one, `draft` or `pending_review`), staff (N30 owner), KYC masked, documents (none), activity, notes. | A-03 | ▢ | |
| MQ-05.17 ★ | Click Request info. Choose the field "Storefront photo" and type "Please add a clear photo of the shop front." Send. | Status `needs_info`. The business leaves the Submitted tab and shows in Needs info. Audit row written. | `R-3.3` A-03 | ▢ | |
| MQ-05.18 ★ | In profile C refresh. | Banner names exactly the storefront photo and shows the admin's message. Nothing else is asked. | `R-3.3` SC-BZ B-02 | ▢ | |
| MQ-05.19 | Upload the JPEG as storefront photo and resubmit. | Status `submitted` again. | `R-3.3` B-02 | | |
| MQ-05.20 ★ | In profile D open the business again, check the document and click Verify. | Status `verified`. Store goes `live` (first store of the business). Audit row. | `R-3.3` `R-3.4` E2E-08 A-03 | ▢ | |
| MQ-05.21 ★ | Within 1 minute, in profile A search "Ganesh" at the QA point. | "Ganesh Stores HAL" appears at about 1.3 km with Amul Butter ₹57. | `R-3.4` SR-01 SC-BZ C-05 | ▢ | |
| MQ-05.22 | In profile C refresh B-03. | Status banner gone. Setup checklist shows KYC done, 50 products (2 of 50), staff (not added), QR standee printed (not yet). | B-03 | ▢ | |
| MQ-05.23 | B-07 → Amul Butter → history. | One "Opening" movement +12 by N30. | `R-4.4` B-07 | | |
| MQ-05.24 | B-10 Credits and referrals. | Balance ₹0; business referral code shown; QR standee section with the store short code, "Download A5 PDF" and preview. | `R-3.12` B-10 | ▢ | |
| MQ-05.25 | Download the PDF and scan its QR with a phone camera (or decode the PNG). | URL `https://app.todayz.localhost/s/<slug>?r=<store short code>&s=<standee id>`; no other code in it. | `R-3.12` ST-01 | | |

### D. Field agent onboards a shop (profile E, F40)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.26 ★ | Sign in to the admin console on the phone as F40 (Rahul). | The sidebar (or phone menu) shows only Create business and My stats. | `R-14.1` AD-02 A-04 | ▢ | |
| MQ-05.27 ★ | Start A-04 Create business for N31: brand "Shree Medicals", category Pharmacy, store at a pin 1.5 km from the QA point, hours 09:00–22:00, owner mobile `+91 90000 00031`. | The phone-first wizard mirrors B-02. | `R-14.2` A-04 | ▢ | |
| MQ-05.28 | Take a storefront photo with the camera control (or pick the JPEG). | Photo uploaded and shown as a thumbnail. | `R-14.2` A-04 | | |
| MQ-05.29 | Add products one by one: Dolo 650 strip ₹30 stock 10 (from master); "QA Herbal Balm 10 g" ₹45 stock 5 (create new). | Master item added. The new product is marked "Pending review" but usable in this store. | `R-4.3` `R-14.2` A-04 | ▢ | |
| MQ-05.30 ★ | Tap "Send OTP to owner". | The app asks for the code from the owner's phone. (N31 is a reserved number: the code is `000000`; with a mock-route number the code shows in the mocks SMS tab.) | `R-14.2` A-04 | ▢ | |
| MQ-05.31 | Enter `111111`. | Refused; attempts left shown. | `R-2.1` A-04 | | |
| MQ-05.32 ★ | Enter `000000`. | Owner confirmed. The business is created with `onboarded_by` = F40, status `submitted`. | `R-14.2` E2E-08 A-04 | ▢ | |
| MQ-05.33 ★ | In profile C sign out and sign in as N31 on the business app. | Banner "Created by Rahul (TodayZ) · confirm". Tapping it shows the business details and a confirm step. | `R-14.2` B-02 | ▢ | |
| MQ-05.34 | Confirm. | Banner changes to the verification status banner. | B-02 | | |
| MQ-05.35 | In profile D (AD47) verify "Shree Medicals". | `verified`; store `live`. Shop appears in profile A search for "Dolo" within 1 minute. | `R-3.4` E2E-08 | ▢ | |
| MQ-05.36 | In profile A place a pickup order at Shree Medicals for Dolo 650 ×1 as C10; accept it in a new profile as N31; hand over. | First order collected. | E2E-08 | | |
| MQ-05.37 | In profile E open My stats. | Shops onboarded 2 (AP and Shree Medicals), verified 2, products added 2, first-order rate shown. Only F40's own row. | `R-14.4` A-15 | ▢ | |

### E. Field agent edit window

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.38 | As F40 edit the price of Dolo 650 at Shree Medicals to ₹31. | Saved. | `R-14.3` FA-01 | | |
| MQ-05.39 | `clock +29d23h`. Edit the price to ₹32. | Saved (inside 30 days of verification, `field_agent_edit_window_days`). | `R-14.3` FA-01 | | |
| MQ-05.40 ★ | `clock +1h1s` (now 30 d + 1 s after verification). Edit again. | Refused. Items show read-only for F40 with a short explanation. The API returns 403. | `R-14.3` FA-01 A-04 | ▢ | |
| MQ-05.41 | `clock reset`. Sign in as F41 (Priya) and open `/v1/admin/field/businesses/<Shree Medicals id>` by Copy as cURL from F40's session with F41's token, or open the A-04 URL. | 403 (not onboarded by F41); the screen shows "You don't have access to this". | `R-14.3` FA-01 | ▢ | |

### F. New branch needs a store review

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-05.42 ★ | In profile C sign in as O20. B-13 → Stores → Add store "Sri Lakshmi 3", address 1.0 km from the QA point, hours 08:00–22:00, one photo. Submit. | Store status `pending_review`. | `R-3.4` SR-01 B-13 | ▢ | |
| MQ-05.43 | In profile A search the area. | "Sri Lakshmi 3" does not appear; its slug URL `/s/<slug>` shows "not found". | `R-3.4` SR-01 C-07 | | |
| MQ-05.44 | In profile D open A-05 Stores → Review. Open Sri Lakshmi 3. Click Send back with "Photo is blurry". | Status back to `draft` with the note. O20 sees the note in B-13. | `R-3.4` A-05 | ▢ | |
| MQ-05.45 | O20 uploads a new photo and resubmits. AD47 approves. | Store `live`; appears in profile A within 1 minute at 1.0 km. | `R-3.4` SR-01 A-05 | ▢ | |

## Cleanup

1. `clock reset`.
2. `make reset` (new businesses and stores remain otherwise).
