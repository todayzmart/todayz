# A11Y Accessibility in detail

MQ-09 walks the customer order with TalkBack and VoiceOver and the admin console with the keyboard. This file adds the business app, contrast, touch targets, focus, motion, forms and errors. Target WCAG 2.2 AA.

- **Covers:** SC-UI, SC-CU, SC-BU (accessibility rows); 06 G-rules.
- **Screens:** C-03, C-05, C-09, C-10, C-12; B-03, B-04, B-05, B-07, B-13; A-07; S-01.
- **Devices:** D1280 with keyboard only; P360 with TalkBack; axe DevTools or Lighthouse in Chrome.
- **Time:** about 90 minutes.

## Preconditions

1. `make reset`. Chrome with the axe DevTools extension (or Lighthouse accessibility audit).

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| A11Y-M.01 ★ | As K22 on D1280, unplug the mouse. Sign in to the business app with the keyboard only. Open Sell (B-05). | Every control reachable by Tab in a sensible order; focus ring visible on every control (mint or dark outline, never invisible). | SC-BU B-05 | ▢ | |
| A11Y-M.02 | On B-05 use the documented shortcuts: focus the scan field, add I2 by typing its barcode and Enter, change quantity, apply round-off, pick cash, complete. | The whole sale without the mouse. After each sale focus returns to the scan field. Record the shortcut list the app shows. | POS-36…55 B-05 | ▢ | |
| A11Y-M.03 | Open B-04 Orders with the keyboard. Accept an order. Open the partial-accept dialog, then press Esc. | Dialog traps focus while open; Esc closes it and focus returns to the button that opened it. | SC-BU B-04 | | |
| A11Y-M.04 | With a screen reader (NVDA or VoiceOver on Mac) let a new order arrive while on B-07. | The new-order toast is announced (live region), not only played as sound. | SC-BU | | |
| A11Y-M.05 | Run axe on C-03, C-05, C-09, C-12, B-03, B-04, B-05, B-07, B-13, A-07 and S-01. | No serious or critical issues. List every issue with its rule id. | SC-UI | ▢ | |
| A11Y-M.06 | Check contrast of: price and MRP strike on C-08, StockTag "updated 12 min ago", the mint active nav item, disabled buttons, the pastel tint tags ("5% back", "In-store only"), and placeholder text. Use the DevTools colour picker. | Text ≥ 4.5:1 (large text ≥ 3:1); non-text indicators ≥ 3:1. List each failing pair with its ratio. | SC-UI | ▢ | |
| A11Y-M.07 | At P360 measure tap targets on C-09 steppers, the C-03 radius chips, the bottom nav, the B-05 number pad and the code entry on B-04. | Each at least 44×44 CSS px (the book's bar) or 24×24 with spacing (WCAG 2.2 minimum). Record the smallest. | SC-UI | | |
| A11Y-M.08 | Turn on "Reduce motion" in the OS. Open C-03 (map) and the C-12 tracking animation. | Animations reduced or removed. | SC-UI | | |
| A11Y-M.09 | With TalkBack on C-10, enter a wrong OTP. | The error is announced and tied to the field (`aria-describedby`); focus moves to it or stays on the field. | C-10 SC-CU | | |
| A11Y-M.10 | With TalkBack, read the C-03 StoreMap card. | The map has a text alternative (shops nearby, open now, products) and the list below is reachable; the map is not a focus trap. | C-03 | | |
| A11Y-M.11 | Zoom the business app to 200% at 1280 px on B-05 and B-07. Then 400% at 320 px width (reflow). | Nothing lost or overlapping at 200%. At 400% the content reflows to one column without horizontal scroll (except the item table, which may scroll inside itself). | SC-BU | ▢ | |
| A11Y-M.12 | Turn on Windows High Contrast (or forced colours in DevTools → Rendering). Open C-09 and B-05. | Buttons, borders, focus and the selected state stay visible. | SC-UI | | |
| A11Y-M.13 | Check the `lang` attribute on `<html>` in each language (C-18 → Hindi, Kannada). | `lang` changes to `hi`, `kn`, `te`, `ta`, so screen readers use the right voice. | `R-1.2` SC-I18N | | |

## Cleanup

1. Turn off OS accessibility settings changed here. `make reset`.
