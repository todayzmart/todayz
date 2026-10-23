import type { Page } from '@playwright/test';

export const TURNSTILE_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js';
/** The leads endpoint of the build:e2e API origin (PUBLIC_API_URL falls back to the local API). */
export const LEADS_URL = 'https://api.todayz.localhost/v1/public/leads';

/** Test phone numbers come only from the reserved +91 90000 000xx block. */
export const TEST_PHONE = '+91 90000 00012';
export const TEST_PHONE_E164 = '+919000000012';

/** Answers the Turnstile script with a stand-in that never renders a token. */
export async function turnstileWithoutToken(page: Page): Promise<void> {
  await page.route(TURNSTILE_URL, (route) =>
    route.fulfill({ contentType: 'text/javascript', body: 'window.turnstile = { reset() {} };' }),
  );
}

/**
 * Answers the Turnstile script with a local stand-in that renders a token into every
 * `.cf-turnstile` box, so tests never touch the network. Register after guardPage().
 */
export async function blockTurnstile(page: Page, token = 'test-token'): Promise<void> {
  await page.route(TURNSTILE_URL, (route) =>
    route.fulfill({
      contentType: 'text/javascript',
      body: `
        window.turnstile = { reset() { document.querySelectorAll('.cf-turnstile input').forEach((i) => { i.value = ''; }); setTimeout(fill, 0); } };
        function fill() {
          document.querySelectorAll('.cf-turnstile').forEach((box) => {
            if (box.querySelector('input')) { box.querySelector('input').value = ${JSON.stringify(token)}; return; }
            const input = document.createElement('input');
            input.type = 'hidden';
            input.name = 'cf-turnstile-response';
            input.value = ${JSON.stringify(token)};
            box.appendChild(input);
          });
        }
        fill();
      `,
    }),
  );
}
