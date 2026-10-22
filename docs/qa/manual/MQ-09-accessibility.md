# MQ-09 Accessibility

A screen reader user walks through first run (MQ-01) and a pickup order (MQ-02) on a phone. A keyboard-only admin works through the console without a mouse. Axe in Playwright covers every route; this script covers what axe cannot hear or feel.

- **Covers:** 08 §5 MQ-09; G2; SC-UI manual side; AU-21…30 manual side (sign-in with a screen reader).
- **Screens:** C-01, C-02, C-03, C-05, C-07, C-08, C-09, C-10, C-11, C-12, C-14; A-01, A-02, A-06, A-07, A-09, A-17.
- **Devices:** real Android phone with TalkBack (Chrome) for sections A and B; real iPhone with VoiceOver (Safari) for section C; D1440 with keyboard only for section D. In the browser pane, use the Chrome accessibility tree (DevTools → Elements → Accessibility) where no real phone is at hand, and mark the step **B** for the real-phone pass.
- **Time:** about 75 minutes.

## Preconditions

1. `make reset`; `clock reset`; run between 10:00 and 18:00 IST.
2. C01 has no account. C10 (wallet 100) exists. K22 is signed in on a desktop profile at Sri Lakshmi to accept the order.
3. TalkBack on, speech rate at the tester's normal speed. Phone language English.
4. For section D: unplug the mouse or turn off the trackpad. Keep a note of every place the focus is lost or hidden.

## What to check on every screen

| Check | Pass when |
|---|---|
| Name | Every control is read with a name that says what it does ("Add Amul Butter 100 g to cart", not "button" or "plus"). Icon-only buttons have a label. |
| Role and state | Buttons read as buttons, tabs as tabs with "selected", toggles with on/off, chips with selected state. |
| Order | Swipe order follows the visual order: top to bottom, start to end. No hidden or off-screen element is read. |
| Live updates | Timers, order status changes and toasts are announced once, without reading every second. |
| Targets | Every tap target is at least 44 × 44 px on the phone (G2). Measure the smallest ones in DevTools. |
| Focus | On desktop, every control is reachable with Tab, in order, with a visible focus ring; Esc closes dialogs and returns focus to the control that opened them. |

## Steps

### A. First run with TalkBack (MQ-01 path, profile A)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-09.01 ★ | Open https://app.todayz.localhost with TalkBack on. | The page title is read. The first focus lands on the language picker heading (C-01). | G2 C-01 | | |
| MQ-09.02 | Swipe through the five languages. | Each is read in its own language, with "button" or "radio button", and the selected one says "selected". Hindi, Kannada, Telugu and Tamil names are read by the Indic voice, not spelled letter by letter. | G2 `R-1.2` C-01 | | |
| MQ-09.03 | Pick English. | Focus moves to the next screen's heading (location set, C-02); the change is announced. | G2 C-02 | | |
| MQ-09.04 | Choose "Use current location". | The explanation before the browser prompt is read in full before the prompt shows. | C-02 | | |
| MQ-09.05 | Deny the permission. | A message is announced that location was not allowed, and focus goes to the PIN field. No dead end. | C-02 | ▢ | |
| MQ-09.06 | Type 560038 with the on-screen keyboard and pick "Indiranagar, 560038". | The PIN field is read as "PIN code, edit box". The suggestion list is announced with its count. | C-02 | | |
| MQ-09.07 ★ | On Home (C-03) swipe from the top. | Order: location label ("Indiranagar, 560038, change location"), search field, StoreMap summary ("4 shops nearby, 4 open now", or similar text, not the map tiles), radius chips, categories, shop cards. The map canvas is either skipped or read as one labelled element, never as dozens of unlabeled pins. | G2 C-03 | ▢ | |
| MQ-09.08 | Double tap the 5 km chip. | Read as "5 km, selected". The stats update is announced once (6 shops nearby). | G2 `R-11.1` C-03 | | |
| MQ-09.09 | Swipe to the Sri Lakshmi shop card. | Read as one item with name, distance 0.6 km, open state and "5% back". The card opens with one double tap. | G2 C-03 | | |
| MQ-09.10 | On the shop page (C-07) swipe to Amul Butter. | Name, pack, price ₹58, "MRP ₹60" (the struck price is read as MRP, not as a second price), stock state and freshness. | G2 `R-4.6` C-07 | | |
| MQ-09.11 | Open the product (C-08). Find the quantity stepper. | Minus and plus are labelled ("Decrease quantity", "Increase quantity") and the current value is read after each change. Each button is at least 44 × 44 px. | G2 C-08 | ▢ | |
| MQ-09.12 | Turn on font size "Largest" in Android settings and reopen C-08. | Text grows; nothing overlaps; Add to cart stays visible without horizontal scroll. | G2 G5 C-08 | ▢ | |

### B. Pickup order with TalkBack (MQ-02 path)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-09.13 ★ | Add Amul Butter ×2 and Maggi ×5 (₹186). Open the cart (C-09). | Each line reads name, quantity and line total. The pickup/delivery switch reads its state. | G2 C-09 | | |
| MQ-09.14 | Go to checkout. Sign in as C10 (C-10). | Phone field reads "+91" as a prefix. The 6 OTP boxes are read as one field "Enter the 6-digit code" or as six labelled boxes with their position ("digit 1 of 6"). The 30 s resend timer is not read every second. | G2 AU-21 C-10 | | |
| MQ-09.15 | Type a wrong OTP `111111`. | The error is announced at once and focus stays in the code field. | G2 `R-2.1` C-10 | | |
| MQ-09.16 | Type `000000`. | Signed in; focus returns to checkout (C-11) with the cart kept. | `R-2.1` C-11 | | |
| MQ-09.17 | Turn credits on. Move the slider. | The slider reads its value in credits ("30 credits") and its range (0 to 30). It can be changed with volume keys or swipe up/down. | G2 `R-7.3` C-11 | | |
| MQ-09.18 ★ | Double tap Place order. | Placed. Focus moves to the status heading on C-12. The countdown is read once on arrival ("Waiting for Sri Lakshmi to accept, 10 minutes left"), then not every second. | G2 `R-5.3` C-12 | ▢ | |
| MQ-09.19 | In the shop profile (K22) accept the order. | The status change is announced on the phone once ("Accepted"). The pickup code is read digit by digit (for example "4 8 1 7", not "four thousand eight hundred…"). | G2 `R-6.1` C-12 | | |
| MQ-09.20 | Swipe to "I'm on my way (+10 min)". | Read as a button with its effect. The hold countdown is read on focus, not on every tick. | G2 `R-5.5` C-12 | | |
| MQ-09.21 | Hand over at the counter with the code (K22). Then open the review (C-14). | "Collected" is announced. The star control reads "Rate 1 to 5 stars" and the chosen value. Tags read with selected state. | G2 C-14 | | |

### C. VoiceOver on iPhone

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-09.22 | Repeat MQ-09.07 to MQ-09.11 with VoiceOver in Safari. | Same names, order and states as with TalkBack. The rotor "Headings" lists the page sections in order. | G2 C-03 C-07 C-08 | | |
| MQ-09.23 | Repeat MQ-09.13 to MQ-09.18 with VoiceOver, signing in as C11. | Same result. The iOS OTP autofill suggestion is reachable and labelled. | G2 C-10 C-11 C-12 | | |
| MQ-09.24 | Turn on Settings → Accessibility → Display → Larger Text at the largest step. Open C-12. | The pickup code, countdown and buttons stay readable and on screen. | G2 C-12 | ▢ | |

### D. Keyboard-only admin (D1440, AD47)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-09.25 ★ | Open https://admin.todayz.localhost. Press Tab from the top. | The first Tab shows a "Skip to content" link. Focus moves through the phone field and the send button with a visible ring. | G2 A-01 | ▢ | |
| MQ-09.26 | Sign in with OTP and TOTP using only the keyboard. | The 6-digit TOTP field accepts typing and Enter submits. | G2 `R-2.5` A-01 | | |
| MQ-09.27 | Tab through the sidebar to A-07 Orders. Press Enter. | The sidebar items are links; the current one is marked (`aria-current`). Focus moves to the page heading. | G2 A-07 | | |
| MQ-09.28 | Use the filter row: type an order number, pick a status with arrow keys, press Enter. | Filters apply; the URL holds them (G6). The result count is announced. | G2 G6 A-07 | | |
| MQ-09.29 | Move through the table rows with Tab or arrow keys and open one with Enter. | Focus never gets stuck in the table. The detail opens and focus moves to its heading. | G2 A-07 | ▢ | |
| MQ-09.30 | Open the admin cancel dialog with the keyboard. Press Esc. | The dialog opens with focus on its first field; Tab stays inside the dialog (focus trap); Esc closes it and focus returns to the Cancel button. | G2 `R-5.14` A-07 | ▢ | |
| MQ-09.31 | Open A-06 Customers, search C10's phone with the keyboard, open the detail. | Reachable and usable. The masked phone is read as masked. | G2 A-06 | | |
| MQ-09.32 | Open A-02 Pulse. Tab through the charts. | Each chart has a text alternative or a data table toggle reachable by keyboard. | G2 A-02 | | |
| MQ-09.33 | Sign in as SA48 and open A-17 Settings. Edit `order_accept_timeout_min` with the keyboard and press Cancel. | The edit field, reason field and step-up dialog are all reachable; Cancel discards the change. | G2 A-17 | | |
| MQ-09.34 | Zoom the browser to 200% on A-07 and A-09. | No content is cut off; no horizontal scroll for text blocks (tables may scroll inside their own box); the sidebar collapses to a menu button that works with Enter. | G2 A-07 A-09 | ▢ | |

## Cleanup

1. Turn off TalkBack and VoiceOver; reset font sizes.
2. `clock reset`; `make reset`.
