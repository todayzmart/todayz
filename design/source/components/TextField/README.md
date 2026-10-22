# TextField

Label above, input, then help or error text below; used for every form in onboarding, checkout and settings.

- Structure: `tz-field` > `tz-field-label`, `tz-input`, then `tz-field-help` or `tz-field-error`. Add `tz-input-invalid` with the error.
- Phone numbers show a fixed "+91" prefix and a numeric keypad (`inputmode="numeric"`).
- Errors say what to fix: "Enter a 6-digit PIN code", never "Invalid input".
- The consumer provides `id`, `for`, the label and the validation message.
