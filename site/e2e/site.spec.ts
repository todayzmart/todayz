// SC-SITE-01…06: the marketing site, as a visitor and a crawler see it. Runs against `astro preview`
// of a `build:e2e` build (local API origin, Turnstile test key).
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { guardPage, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { LEADS_URL, TEST_PHONE, TEST_PHONE_E164, TURNSTILE_URL, blockTurnstile, turnstileWithoutToken } from './helpers.ts';

const EN_PAGES = [
  '/',
  '/for-retailers/',
  '/how-it-works/',
  '/pricing/',
  '/about/',
  '/contact/',
  '/legal/privacy/',
  '/legal/terms-for-shoppers/',
  '/legal/terms-for-shops/',
  '/legal/refunds-and-credits/',
];
const LOCALISED = ['hi', 'kn', 'te', 'ta'] as const;
const DIST = join(import.meta.dirname, '..', 'dist');

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await blockTurnstile(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

/** Fills the lead form with valid values. */
async function fillLead(form: Locator, city = 'Bengaluru'): Promise<void> {
  await form.getByLabel('Your name').fill('Asha Rao');
  await form.getByLabel('Shop name').fill('Rao General Store');
  await form.getByLabel('Mobile number').fill(TEST_PHONE);
  await form.getByLabel('City').fill(city);
}

/** Opens a page and returns its first lead form once the script has enabled the button. */
async function leadForm(page: Page, path: string): Promise<Locator> {
  await page.goto(path);
  const form = page.locator('form[data-lead-form]').first();
  await expect(form.getByRole('button', { name: 'Send details' })).toBeEnabled();
  return form;
}

test.describe('SC-SITE-01 every page is a real, complete document', () => {
  for (const path of EN_PAGES) {
    test(`${path} has one h1, a canonical, Open Graph and no sideways scroll at 390 px`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      const res = await page.goto(path);
      expect(res?.status()).toBe(200);
      await expect(page.locator('h1')).toHaveCount(1);
      await expect(page.locator('main#main')).toHaveCount(1);
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', `https://todayz.in${path}`);
      await expect(page.locator('meta[property="og:image"]')).toHaveAttribute('content', /^https:\/\/todayz\.in\/og\/[a-z-]+\.png$/);
      await expect(page.locator('meta[name="description"]')).toHaveAttribute('content', /.{40,}/);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, 'horizontal overflow in px').toBeLessThanOrEqual(0);
    });
  }

  for (const locale of LOCALISED) {
    test(`/${locale}/ has no sideways scroll at 390 px`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      for (const path of [`/${locale}/`, `/${locale}/for-retailers/`]) {
        await page.goto(path);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        expect(overflow, `${path} horizontal overflow in px`).toBeLessThanOrEqual(0);
      }
    });
  }

  test('an unknown path gets the noindex 404 page', async ({ page }) => {
    const res = await page.goto('/no-such-page/');
    expect(res?.status()).toBe(404);
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
    await expect(page.getByRole('link', { name: /home/i }).first()).toBeVisible();
    // The 404 has no form, so "Get started" goes to the one on the retailers page.
    await expect(page.locator('header a.start')).toHaveAttribute('href', '/for-retailers/#join');
    guard.errors.length = 0; // the browser logs the 404 itself
  });
});

test.describe('SC-SITE-02 lead form: a valid submission', () => {
  test('posts the camelCase payload with the phone in E.164 and the Turnstile token, then confirms', async ({ page }) => {
    let body: Record<string, unknown> | null = null;
    await page.route(LEADS_URL, async (route) => {
      body = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({ status: 201, contentType: 'application/json', body: '{}' });
    });
    const form = await leadForm(page, '/');
    await expect(form).toHaveAttribute('data-density', 'touch');
    await fillLead(form);
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toHaveText('Thank you. We have your details.');
    expect(body).toEqual({
      name: 'Asha Rao',
      shopName: 'Rao General Store',
      phone: TEST_PHONE_E164,
      city: 'Bengaluru',
      source: 'site:home:en',
      turnstileToken: 'test-token',
    });
    await expect(form.getByLabel('Your name')).toHaveValue('');
  });

  test('the localised form sends its own source and the header button goes to it', async ({ page }) => {
    let body: Record<string, unknown> | null = null;
    await page.route(LEADS_URL, async (route) => {
      body = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({ status: 201, body: '{}' });
    });
    await page.goto('/hi/for-retailers/');
    await expect(page.locator('header a.start')).toHaveAttribute('href', '#join');
    const form = page.locator('#join form[data-lead-form]');
    await expect(form.locator('button[type="submit"]')).toBeEnabled();
    await expect(form.locator('.cf-turnstile')).toHaveAttribute('data-language', 'hi');
    await form.locator('input[name="name"]').fill('Asha Rao');
    await form.locator('input[name="shopName"]').fill('Rao General Store');
    await form.locator('input[name="phone"]').fill('0091 90000 00012');
    await form.locator('input[name="city"]').fill('Mysuru');
    await form.locator('button[type="submit"]').click();
    await expect.poll(() => body?.['source']).toBe('site:retailers:hi');
    expect(body?.['phone']).toBe(TEST_PHONE_E164);
  });

  test('Turnstile uses its own strings for en and hi, and the browser language otherwise', async ({ page }) => {
    for (const [path, language] of [
      ['/', 'en'],
      ['/hi/', 'hi'],
      ['/kn/', 'auto'],
      ['/te/', 'auto'],
      ['/ta/', 'auto'],
    ] as const) {
      await page.goto(path);
      await expect(page.locator('.cf-turnstile'), path).toHaveAttribute('data-language', language);
    }
  });

  test('the Turnstile script is requested on the form pages only', async ({ page }) => {
    const seen: string[] = [];
    page.on('request', (r) => {
      if (r.url() === TURNSTILE_URL) seen.push(r.url());
    });
    await page.goto('/about/');
    await page.waitForLoadState('networkidle');
    expect(seen).toEqual([]);
    await page.goto('/contact/');
    await page.waitForLoadState('networkidle');
    expect(seen).toHaveLength(1);
  });
});

test.describe('SC-SITE-03 lead form: bad input and server trouble', () => {
  test('the button renders disabled and only the script enables it', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL: baseURL ?? '' });
    const noJs = await context.newPage();
    await noJs.goto('/contact/');
    await expect(noJs.locator('form[data-lead-form] button[type="submit"]')).toBeDisabled();
    // The <noscript> note explains why (Chromium with JS turned off by CDP still parses it as script-on).
    await expect(noJs.locator('form[data-lead-form] noscript')).toHaveCount(1);
    await context.close();
  });

  test('shows field errors and sends nothing for an invalid form', async ({ page }) => {
    let calls = 0;
    await page.route(LEADS_URL, (route) => {
      calls += 1;
      return route.fulfill({ status: 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    await form.getByLabel('Mobile number').fill('12345');
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(page.getByText('Enter your name.')).toBeVisible();
    await expect(page.getByText('Enter a 10-digit mobile number.')).toBeVisible();
    await expect(form.getByLabel('Your name')).toBeFocused();
    await expect(form.getByLabel('Your name')).toHaveAttribute('aria-invalid', 'true');
    expect(calls).toBe(0);
  });

  test('asks for the check and sends nothing while Turnstile has no token', async ({ page }) => {
    await turnstileWithoutToken(page);
    let calls = 0;
    await page.route(LEADS_URL, (route) => {
      calls += 1;
      return route.fulfill({ status: 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    await fillLead(form);
    await form.getByRole('button', { name: 'Send details' }).click();
    const check = form.locator('[data-error-for="captcha"]');
    await expect(check).toHaveText('Please complete the check above.');
    await expect(check).toBeVisible();
    await expect(form.getByRole('status'), 'the missing check is announced').toHaveText('Please complete the check above.');
    const widget = form.locator('.cf-turnstile');
    await expect(widget, 'only the token is missing: focus goes to the check').toBeFocused();
    await expect(widget).toHaveAttribute('aria-describedby', (await check.getAttribute('id')) ?? 'missing id');
    await expect(form.getByLabel('Your name')).not.toHaveAttribute('aria-describedby');
    expect(calls).toBe(0);
  });

  test('disables the button while the request is in flight', async ({ page }) => {
    let release: () => void = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    let calls = 0;
    await page.route(LEADS_URL, async (route) => {
      calls += 1;
      await held;
      await route.fulfill({ status: 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    const button = form.getByRole('button', { name: 'Send details' });
    await fillLead(form);
    await button.click();
    await expect(form.getByRole('status')).toHaveText('Sending…');
    await expect(button).toBeDisabled();
    await form.evaluate((f: HTMLFormElement) => f.requestSubmit());
    release();
    await expect(form.getByRole('status')).toHaveText('Thank you. We have your details.');
    await expect(button).toBeEnabled();
    expect(calls, 'a second submit while sending sends nothing').toBe(1);
  });

  const failures = [
    ['rate limited (429)', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.fulfill({ status: 429, body: '{}' }), 'Something went wrong'],
    ['down (503)', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.fulfill({ status: 503, body: '{}' }), 'Something went wrong'],
    ['unreachable', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.abort('failed'), 'Something went wrong'],
    ['missing (404)', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.fulfill({ status: 404, body: '{}' }), 'Something went wrong'],
    ['forbidding the origin (403)', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.fulfill({ status: 403, body: '{}' }), 'Something went wrong'],
    ['refusing the details (400)', (r: Parameters<Parameters<Page['route']>[1]>[0]) => r.fulfill({ status: 400, body: '{}' }), 'We could not accept these details.'],
  ] as const;

  for (const [what, answer, message] of failures) {
    test(`says so plainly when the API is ${what}, and lets the visitor retry`, async ({ page }) => {
      await page.route(LEADS_URL, answer);
      const form = await leadForm(page, '/for-retailers/');
      await fillLead(form, 'Mysuru');
      await form.getByRole('button', { name: 'Send details' }).click();
      await expect(form.getByRole('status')).toContainText(message);
      await expect(form.getByRole('button', { name: 'Send details' })).toBeEnabled();
      await expect(form.getByLabel('Your name')).toHaveValue('Asha Rao');
      guard.errors.length = 0; // the browser logs the failed request itself
    });
  }
});

test.describe('SC-SITE-03 lead form: errors are announced, and the form always recovers', () => {
  test('an input points at its error only while the error shows', async ({ page }) => {
    const form = await leadForm(page, '/contact/');
    const name = form.getByLabel('Your name');
    await expect(name).not.toHaveAttribute('aria-describedby');
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(name).toHaveAttribute('aria-describedby', 'lead-name-err');
    await expect(page.locator('#lead-name-err')).toHaveText('Enter your name.');
    await fillLead(form);
    await page.route(LEADS_URL, (route) => route.fulfill({ status: 503, body: '{}' }));
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toContainText('Something went wrong');
    for (const label of ['Your name', 'Mobile number', 'Shop name', 'City']) {
      await expect(form.getByLabel(label), label).not.toHaveAttribute('aria-describedby');
      await expect(form.getByLabel(label), label).toHaveAttribute('aria-invalid', 'false');
    }
    await expect(form.locator('.cf-turnstile')).not.toHaveAttribute('aria-describedby');
    guard.errors.length = 0;
  });

  test('says the security check could not load when the Turnstile script is blocked', async ({ page }) => {
    await page.route(TURNSTILE_URL, (route) => route.abort('blockedbyclient'));
    let calls = 0;
    await page.route(LEADS_URL, (route) => {
      calls += 1;
      return route.fulfill({ status: 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    await fillLead(form);
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toHaveText(
      'Something went wrong. Please try again in a moment. The security check could not load. Reload the page and try again.',
    );
    await expect(form.locator('[data-error-for="captcha"]')).toBeHidden();
    await expect(form.getByRole('button', { name: 'Send details' })).toBeEnabled();
    expect(calls).toBe(0);
    guard.errors.length = 0; // the browser logs the blocked script
  });

  test("says so when the Turnstile widget reports an error", async ({ page }) => {
    await page.route(TURNSTILE_URL, (route) =>
      route.fulfill({
        contentType: 'text/javascript',
        body: `window.turnstile = { reset() {} };
          const wait = setInterval(() => { if (window.tzTurnstileError) { clearInterval(wait); window.tzTurnstileError('300030'); } }, 20);`,
      }),
    );
    let calls = 0;
    await page.route(LEADS_URL, (route) => {
      calls += 1;
      return route.fulfill({ status: 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    await expect(form.getByRole('status')).toContainText('The security check could not load.');
    await fillLead(form);
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toContainText('Something went wrong');
    await expect(form.getByRole('status')).toContainText('The security check could not load.');
    expect(calls).toBe(0);
  });

  test('gives up after 15 s, keeps the details and lets the visitor retry', async ({ page }) => {
    await page.clock.install();
    let release: () => void = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(LEADS_URL, async (route) => {
      await held;
      await route.fulfill({ status: 201, body: '{}' }).catch(() => undefined);
    });
    const form = await leadForm(page, '/contact/');
    const button = form.getByRole('button', { name: 'Send details' });
    await fillLead(form);
    await button.click();
    await expect(form.getByRole('status')).toHaveText('Sending…');
    await page.clock.runFor(14_000);
    await expect(button).toBeDisabled();
    await page.clock.runFor(1_000);
    await expect(form.getByRole('status')).toContainText('Something went wrong');
    await expect(button).toBeEnabled();
    await expect(form.getByLabel('Your name')).toHaveValue('Asha Rao');
    await expect(form.getByLabel('Mobile number')).toHaveValue(TEST_PHONE);
    release();
    guard.errors.length = 0;
  });

  test('sends one Idempotency-Key per set of details: the same on a retry, a new one after a success', async ({ page }) => {
    const keys: string[] = [];
    const answers = [503, 503, 201, 201];
    await page.route(LEADS_URL, (route) => {
      keys.push(route.request().headers()['idempotency-key'] ?? '');
      return route.fulfill({ status: answers[keys.length - 1] ?? 201, body: '{}' });
    });
    const form = await leadForm(page, '/contact/');
    const button = form.getByRole('button', { name: 'Send details' });
    const status = form.getByRole('status');
    await fillLead(form);
    await button.click();
    await expect(status).toContainText('Something went wrong');
    await button.click();
    await expect.poll(() => keys.length).toBe(2);
    await expect(status).toContainText('Something went wrong');
    await button.click();
    await expect(status).toHaveText('Thank you. We have your details.');
    await fillLead(form);
    await button.click();
    await expect.poll(() => keys.length).toBe(4);
    for (const key of keys) expect(key).toMatch(/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/);
    expect(keys[1], 'a retry reuses the key').toBe(keys[0]);
    expect(keys[2], 'a retry reuses the key').toBe(keys[0]);
    expect(keys[3], 'a new lead after a success gets a new key').not.toBe(keys[0]);
    guard.errors.length = 0;
  });
});

test.describe('SC-SITE-06 motion can be stopped (WCAG 2.2.2)', () => {
  test('the shop-type strip has a visible pause control that works from the keyboard', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto('/');
    const track = page.locator('.marquee-track').first();
    await expect(track).toHaveCSS('animation-play-state', 'running');
    const pause = page.getByRole('checkbox', { name: 'Pause the moving list' });
    await expect(page.locator('.marquee-pause').first()).toBeVisible();
    await pause.focus();
    await page.keyboard.press('Space');
    await expect(pause).toBeChecked();
    await expect(track).toHaveCSS('animation-play-state', 'paused');
    await page.keyboard.press('Space');
    await expect(track).toHaveCSS('animation-play-state', 'running');
  });

  test('no other animation repeats forever', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    for (const path of ['/', '/for-retailers/']) {
      await page.goto(path);
      const endless = await page.evaluate(() =>
        document
          .getAnimations()
          .filter((a) => a.effect?.getTiming().iterations === Infinity)
          .map((a) => (a.effect instanceof KeyframeEffect && a.effect.target instanceof Element ? a.effect.target.className : '?')),
      );
      expect(endless.filter((c) => !String(c).includes('marquee-track')), path).toEqual([]);
    }
  });
});

test.describe('SC-SITE-04 languages', () => {
  for (const locale of LOCALISED) {
    // The site translations were reviewed and approved (founder, 25 Oct; W4-05), so no locale is a
    // machine draft any more: no draft meta or notice, and the switcher is the way back to English.
    test(`/${locale}/ is a reviewed translation with hreflang and a way back to English`, async ({ page }) => {
      await page.goto(`/${locale}/`);
      await expect(page.locator('html')).toHaveAttribute('lang', new RegExp(`^${locale}-IN$`));
      await expect(page.locator('meta[name="todayz:translation"]')).toHaveCount(0);
      await expect(page.locator('.draft')).toHaveCount(0);
      await expect(page.locator('link[rel="alternate"][hreflang]')).toHaveCount(6);
      await expect(page.locator('link[rel="alternate"][hreflang="x-default"]')).toHaveAttribute('href', 'https://todayz.in/');
      await expect(page.locator('header details.lang a[href="/"]')).toHaveCount(1);
      await expect(page.locator('h1')).not.toHaveText(/Everything nearby/);
      // Pages that exist only in English are marked as such where a localised page links to them.
      await expect(page.locator('footer a[href="/pricing/"]')).toHaveAttribute('hreflang', 'en');
      await expect(page.locator('footer a[href="/for-retailers/"]')).toHaveCount(0);
      await page.goto(`/${locale}/for-retailers/`);
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', `https://todayz.in/${locale}/for-retailers/`);
    });
  }

  test('the English pages are not marked as drafts and list the language switcher', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('meta[name="todayz:translation"]')).toHaveCount(0);
    await expect(page.locator('.draft')).toHaveCount(0);
    await expect(page.locator('header details.lang a[hreflang]')).toHaveCount(5);
    await expect(page.locator('footer a[href="/pricing/"]')).not.toHaveAttribute('hreflang', /./);
  });

  test('pages that exist only in English offer no language switcher', async ({ page }) => {
    await page.goto('/pricing/');
    await expect(page.locator('header details.lang')).toHaveCount(0);
    await expect(page.locator('header a.start')).toHaveAttribute('href', '/for-retailers/#join');
  });
});

test.describe('SC-SITE-05 search engines', () => {
  test('sitemap lists every page with alternates and robots points at it', async ({ request }) => {
    const sitemap = await (await request.get('/sitemap.xml')).text();
    for (const p of EN_PAGES) expect(sitemap).toContain(`<loc>https://todayz.in${p}</loc>`);
    for (const l of LOCALISED) expect(sitemap).toContain(`<loc>https://todayz.in/${l}/</loc>`);
    expect(sitemap).not.toContain('/404');
    const robots = await (await request.get('/robots.txt')).text();
    expect(robots).toContain('Sitemap: https://todayz.in/sitemap.xml');
  });

  test('home carries Organization, WebSite and FAQPage JSON-LD that parses', async ({ page }) => {
    await page.goto('/');
    const blocks = await page.locator('script[type="application/ld+json"]').allTextContents();
    const types = blocks.map((b) => (JSON.parse(b) as { '@type': string })['@type']).sort();
    expect(types).toEqual(['FAQPage', 'Organization', 'WebSite']);
  });

  test('the retailers page carries SoftwareApplication and FAQPage JSON-LD', async ({ page }) => {
    await page.goto('/for-retailers/');
    const blocks = await page.locator('script[type="application/ld+json"]').allTextContents();
    const types = blocks.map((b) => (JSON.parse(b) as { '@type': string })['@type']).sort();
    expect(types).toEqual(['FAQPage', 'SoftwareApplication']);
  });

  // `astro preview` does not serve Cloudflare's _redirects and _headers, so these read the built files.
  test('the old landing page URL is covered by a 301 in _redirects', () => {
    const file = readFileSync(join(DIST, '_redirects'), 'utf8');
    expect(file).toContain('/todayz-landing-page.html / 301');
  });

  test('_headers sets the CSP and the other security headers for every path', () => {
    const file = readFileSync(join(DIST, '_headers'), 'utf8');
    expect(file).toMatch(/^\/\*$/m);
    expect(file).toContain("script-src 'self' https://challenges.cloudflare.com");
    expect(file).toContain('connect-src \'self\' https://api.todayz.localhost');
    expect(file).toContain("frame-ancestors 'none'");
    expect(file).toContain('X-Content-Type-Options: nosniff');
    expect(file).toContain('Referrer-Policy: strict-origin-when-cross-origin');
    expect(file).toContain('/_astro/*\n  Cache-Control: public, max-age=31536000, immutable');
  });
});

test.describe('SC-SITE-06 keyboard, no-JS content and restraint', () => {
  test('skip link, mobile menu and FAQ work from the keyboard', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/');
    await page.keyboard.press('Tab');
    await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('main#main')).toBeFocused();
    await page.locator('summary[aria-label="Menu"]').click();
    await expect(page.locator('details.narrow').getByRole('link', { name: 'Pricing' })).toBeVisible();
    // The landing FAQ opens its first answer; the second starts closed.
    const second = page.locator('.faq details').nth(1);
    await expect(second).not.toHaveAttribute('open');
    await second.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(second).toHaveAttribute('open', '');
  });

  test('all copy renders without JavaScript', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL: baseURL ?? '' });
    const page = await context.newPage();
    for (const path of ['/', '/for-retailers/']) {
      await page.goto(path);
      await expect(page.locator('h1')).toBeVisible();
      await expect(page.locator('.faq summary').first()).toBeVisible();
    }
    await context.close();
  });

  test('no emoji in the rendered text', async ({ page }) => {
    for (const path of EN_PAGES) {
      await page.goto(path);
      const text = await page.locator('body').innerText();
      expect(text.replace(/[©®™]/g, ''), path).not.toMatch(/\p{Extended_Pictographic}/u);
    }
  });

  test('the landing pages say their shops and numbers are examples, and claim no commission', async ({ page }) => {
    for (const path of ['/', '/for-retailers/']) {
      await page.goto(path);
      await expect(page.getByText('Shops, items and numbers on this page are examples.')).toBeVisible();
      const text = await page.locator('main').innerText();
      expect(text, path).not.toMatch(/commission/i);
      expect(text, path).not.toMatch(/\b\d[\d,]*\+/);
    }
  });

  test('the mock screens are hidden from assistive technology', async ({ page }) => {
    await page.goto('/');
    const mocks = page.locator('main [aria-hidden="true"]');
    expect(await mocks.count()).toBeGreaterThan(0);
    // No mock exposes a focusable control.
    expect(await page.locator('main [aria-hidden="true"] :is(a, button, input, select, textarea)').count()).toBe(0);
  });
});
