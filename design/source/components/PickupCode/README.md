# PickupCode

The 4-digit code a customer shows at the counter, and the four-box input the shop types it into.

- Customer side: `tz-code` on the black `inverse` card, so the code reads like a ticket, digits in `otp`, the shop name and "Show this code at the counter". Brightness hint: the screen should stay awake while it is shown.
- Shop side: `tz-otp`, four numeric inputs that auto-advance and accept paste. Wrong code shakes once and says "That code doesn't match order TZ-24817".
- Codes are single-use and expire when the order closes.
