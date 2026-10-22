# FAIL Failure modes

What happens when something breaks in the middle of a job: the database, the worker, the mocks, MinIO, the network, the clock, or the person (double taps, back button, two tabs). Each step checks three things: the user sees a clear message with "Try again", nothing is done twice, and nothing is lost.

- **Covers:** AU-08; BK-04, BK-35; 06 G1 error and offline states; SC-CU, SC-BU, SC-OB, SC-AI (failure rows); R-5.19, R-7.5, R-17.3.
- **Screens:** C-09, C-10, C-11, C-12; B-04, B-05, B-06, B-07; A-08.
- **Devices:** P360 (A, B), D1280 (B), D1440 (D). A terminal for the stop and start commands.
- **Time:** about 90 minutes.

## Preconditions

1. `make dev`, then `make reset`. Mocks UI open.
2. Service commands: `docker compose -p todayz stop <service>` and `start <service>` for postgres and minio; `infra/local/proc.sh stop|start <api|worker|mocks>` for host processes (record the exact names the script uses).
3. After each step, start what you stopped and wait for https://todayz.localhost/_health to show all green before the next step.

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| FAIL-M.01 ★ | C10 on C-11 with a cart at SL and 30 credits. Stop Postgres. Tap Place order. | An error with "Try again" (06 G1), no blank screen, no endless spinner. The API returns `503` (record status and `code`). | 06 G1 | ▢ | |
| FAIL-M.02 ★ | Start Postgres. Tap "Try again" on the same screen. | Exactly one order (same `Idempotency-Key` reused). C-15 held 30 once. A-07 shows one order. | BK-04 `R-7.5` | ▢ | |
| FAIL-M.03 | On C-11 double-tap Place order fast (or click twice in DevTools with a throttled network). | One order. The button disables after the first tap. Network shows either one request, or two with the same key and the same response. | BK-04 | | |
| FAIL-M.04 | Replay the place request from DevTools with the same `Idempotency-Key` and a changed quantity. | `422 idempotency_mismatch`; no second order. | BK-04 `idempotency_mismatch` | | |
| FAIL-M.05 | After placing, press the browser Back button twice. | C-12 or the cart shows the placed state; the cart is empty or marked placed. Going forward again does not resubmit the order. | SC-CU | | |
| FAIL-M.06 | Open C-11 in two tabs as C10 with the same cart. Place in tab 1, then in tab 2. | Tab 2 shows the existing order or `account_has_active_order` if the rule applies (record), never a duplicate with double holds. | `account_has_active_order` `R-5.24` | | |
| FAIL-M.07 | On C-10 request an OTP, refresh the page before typing it, then type the OTP from the mocks tab. | The page remembers the number (or asks again); the same OTP still works within 5 minutes; the cart is kept. | AU-01 SC-CU | | |
| FAIL-M.08 ★ | As C10 with a cart: shorten the access token (devtools: delete the access cookie only, keep refresh). Tap Checkout. | Silent refresh once, the request retries and succeeds; no sign-in screen. | AU-08 | | |
| FAIL-M.09 | Now delete both cookies (simulate a failed refresh) and tap Place order. | Sign-in screen (C-10). After sign-in the same cart and checkout return. | AU-08 `R-2.1` | ▢ | |
| FAIL-M.10 | C10 tracks an accepted order on C-12. Stop the API for 30 s. K22 cannot act during it; start the API, then K22 marks Ready. | C-12 shows a reconnecting state while the API is down, then "Ready" within a few seconds of the event (SSE replay, no reload needed). | BK-35 | ▢ | |
| FAIL-M.11 ★ | Place 2 orders at SL. Stop the worker. `clock +11m`. Check both orders; then start the worker. | While stopped: both stay `placed` (late timer). After start: both expire within seconds, credits released once each. No order is skipped. | OB-05 `R-5.3` | | |
| FAIL-M.12 | Stop the mocks server. Sign in a new customer C05 (OTP send). | A clear error ("Couldn't send the code. Try again" or the build's translated text); `503`-class status with a stable `code`. No OTP in any log. Start the mocks; the resend works. | `R-2.1` PL-02 | | |
| FAIL-M.13 | Mocks stopped. C10 places an order at SL (push and SMS to the shop fail). | The order is placed; the shop still gets it on B-04 through SSE and the inbox. Dashboard 04 shows notification failures with retries. | `R-15.1` SC-NT | | |
| FAIL-M.14 | Stop MinIO. As O20 upload a product photo on B-07. | A clear upload error with "Try again"; the product saves without the photo or is not saved (record). Start MinIO; retry succeeds. | 06 G1 SC-BU | | |
| FAIL-M.15 | With `ai_catalogue_enabled` on, select the mock fixture `mock_overloaded` (529). As G43 open an A-08 review item with the assist panel. | The assist panel shows a calm "Assistant not available" state; the rest of A-08 works. No retry storm in the logs (record the retry count). | `R-17.3` SC-AI | | |
| FAIL-M.16 | Same 529 fixture for voice (B-06 draft). | The voice draft fails with a retry option; no stock changes. | `R-12.7` `R-17.3` | | |
| FAIL-M.17 | Set the phone clock 2 hours ahead (OS settings) and open the customer app; place and track an order. | Times shown come from the server (accept-by countdown correct). Record any screen that uses the device clock. | `R-5.3` SC-CU | | |
| FAIL-M.18 | K22 on B-05 with a bill of 5 lines. Close the tab before payment. Reopen Sell. | The draft bill is restored or clearly gone (record). No stock moved and no sale recorded. | SC-BU `R-12.9` | | |
| FAIL-M.19 | Two cashiers (K22 on D1280, M21 on P360) accept the same order at the same moment. | One wins; the other sees "This order was already accepted" or `invalid_transition` (409). One set of reservations. | BK-32 `invalid_transition` | | |
| FAIL-M.20 | Network drop mid-accept: K22 throttles to Offline after tapping Accept, then back online. | The accept either landed (shown accepted after reconnect) or shows "Try again"; never a stuck spinner. A retry with the same key does not double reserve. | `R-5.19` BK-04 | | |

## Cleanup

1. Start every stopped service. Reset the phone clock. Select the normal LLM fixture. `clock reset`; `make reset`.
