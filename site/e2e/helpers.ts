import type { Page } from '@playwright/test';

export const TURNSTILE_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js';
export const LEADS_URL = 'https://api.todayz.in/v1/public/leads';

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
