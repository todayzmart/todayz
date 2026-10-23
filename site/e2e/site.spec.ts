// SC-SITE-01…06: the marketing site, as a visitor and a crawler see it. Runs against `astro preview`.
import { guardPage, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { LEADS_URL, TURNSTILE_URL, blockTurnstile } from './helpers.ts';

const EN_PAGES = [
  '/',
  '/for-retailers',
  '/how-it-works',
  '/pricing',
  '/about',
  '/contact',
  '/legal/privacy',
  '/legal/terms-for-shoppers',
  '/legal/terms-for-shops',
  '/legal/refunds-and-credits',
];
const LOCALISED = ['hi', 'kn', 'te', 'ta'] as const;

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await blockTurnstile(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test.describe('SC-SITE-01 every page is a real, complete document', () => {
  for (const path of EN_PAGES) {
    test(`${path} has one h1, a canonical, Open Graph and no sideways scroll at 390 px`, async ({ page }) => {
      await page.setViewportSize({ width: 390, height: 844 });
      const res = await page.goto(path);
      expect(res?.status()).toBe(200);
      await expect(page.locator('h1')).toHaveCount(1);
      await expect(page.locator('main#main')).toHaveCount(1);
      const canonical = await page.locator('link[rel="canonical"]').getAttribute('href');
      expect(canonical).toBe(`https://todayz.in${path === '/' ? '/' : path}`);
      await expect(page.locator('meta[property="og:image"]')).toHaveAttribute('content', /^https:\/\/todayz\.in\/og\/[a-z-]+\.png$/);
      await expect(page.locator('meta[name="description"]')).toHaveAttribute('content', /.{40,}/);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, 'horizontal overflow in px').toBeLessThanOrEqual(0);
    });
  }

  test('an unknown path gets the noindex 404 page', async ({ page }) => {
    const res = await page.goto('/no-such-page');
    expect(res?.status()).toBe(404);
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
    await expect(page.getByRole('link', { name: /home/i }).first()).toBeVisible();
    guard.errors.length = 0; // the browser logs the 404 itself
  });
});

test.describe('SC-SITE-02 lead form: a valid submission', () => {
  test('posts the typed payload with the Turnstile token and confirms', async ({ page }) => {
    let body: Record<string, unknown> | null = null;
    await page.route(LEADS_URL, async (route) => {
      body = route.request().postDataJSON() as Record<string, unknown>;
      await route.fulfill({ status: 201, contentType: 'application/json', body: '{}' });
    });
    await page.goto('/');
    const form = page.locator('form[data-lead-form]');
    await form.getByLabel('Your name').fill('Asha Rao');
    await form.getByLabel('Shop name').fill('Rao General Store');
    await form.getByLabel('Mobile number').fill('+91 98765 43210');
    await form.getByLabel('City').fill('Bengaluru');
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toHaveText('Thank you. We have your details.');
    expect(body).toEqual({
      name: 'Asha Rao',
      shop_name: 'Rao General Store',
      phone: '9876543210',
      city: 'Bengaluru',
      source: 'site:home:en',
      turnstile_token: 'test-token',
    });
  });

  test('the Turnstile script is requested on the form pages only', async ({ page }) => {
    const seen: string[] = [];
    page.on('request', (r) => {
      if (r.url() === TURNSTILE_URL) seen.push(r.url());
    });
    await page.goto('/about');
    await page.waitForLoadState('networkidle');
    expect(seen).toEqual([]);
    await page.goto('/contact');
    await page.waitForLoadState('networkidle');
    expect(seen).toHaveLength(1);
  });
});

test.describe('SC-SITE-03 lead form: bad input and server trouble', () => {
  test('shows field errors and sends nothing for an invalid form', async ({ page }) => {
    let calls = 0;
    await page.route(LEADS_URL, (route) => {
      calls += 1;
      return route.fulfill({ status: 201, body: '{}' });
    });
    await page.goto('/contact');
    const form = page.locator('form[data-lead-form]');
    await form.getByLabel('Mobile number').fill('12345');
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(page.getByText('Enter your name.')).toBeVisible();
    await expect(page.getByText('Enter a 10-digit mobile number.')).toBeVisible();
    await expect(form.getByLabel('Your name')).toBeFocused();
    expect(calls).toBe(0);
  });

  test('says so plainly when the API is rate limited or down, and lets the visitor retry', async ({ page }) => {
    await page.route(LEADS_URL, (route) => route.fulfill({ status: 429, body: '{}' }));
    await page.goto('/for-retailers');
    const form = page.locator('form[data-lead-form]').first();
    await form.getByLabel('Your name').fill('Asha Rao');
    await form.getByLabel('Shop name').fill('Rao General Store');
    await form.getByLabel('Mobile number').fill('9876543210');
    await form.getByLabel('City').fill('Mysuru');
    await form.getByRole('button', { name: 'Send details' }).click();
    await expect(form.getByRole('status')).toContainText('Something went wrong');
    await expect(form.getByRole('button', { name: 'Send details' })).toBeEnabled();
    await expect(form.getByLabel('Your name')).toHaveValue('Asha Rao');
    guard.errors.length = 0; // the browser logs the 429 itself
  });
});

test.describe('SC-SITE-04 languages', () => {
  for (const locale of LOCALISED) {
    test(`/${locale}/ is a machine-draft translation with hreflang and a way back to English`, async ({ page }) => {
      await page.goto(`/${locale}/`);
      await expect(page.locator('html')).toHaveAttribute('lang', new RegExp(`^${locale}-IN$`));
      await expect(page.locator('meta[name="todayz:translation"]')).toHaveAttribute('content', 'machine-draft');
      await expect(page.locator('link[rel="alternate"][hreflang]')).toHaveCount(6);
      await expect(page.locator('link[rel="alternate"][hreflang="x-default"]')).toHaveAttribute('href', 'https://todayz.in/');
      await expect(page.locator('.draft a')).toHaveAttribute('href', '/');
      await expect(page.locator('h1')).not.toHaveText('Everything nearby, today.');
      await page.goto(`/${locale}/for-retailers`);
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', `https://todayz.in/${locale}/for-retailers`);
    });
  }

  test('the English pages are not marked as drafts and list the language switcher', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('meta[name="todayz:translation"]')).toHaveCount(0);
    await expect(page.locator('.draft')).toHaveCount(0);
    const links = page.locator('header details a[hreflang]');
    await expect(links).toHaveCount(5);
  });

  test('pages that exist only in English offer no language switcher', async ({ page }) => {
    await page.goto('/pricing');
    await expect(page.locator('header details a[hreflang]')).toHaveCount(0);
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
    await page.goto('/for-retailers');
    const blocks = await page.locator('script[type="application/ld+json"]').allTextContents();
    const types = blocks.map((b) => (JSON.parse(b) as { '@type': string })['@type']).sort();
    expect(types).toEqual(['FAQPage', 'SoftwareApplication']);
  });

  test('the old landing page URL is covered by a 301 in _redirects', async ({ request }) => {
    const file = await (await request.get('/_redirects')).text();
    expect(file).toContain('/todayz-landing-page.html / 301');
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
    const first = page.locator('.faq summary').first();
    await first.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('.faq details').first()).toHaveAttribute('open', '');
  });

  test('all copy renders without JavaScript', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL: baseURL ?? '' });
    const page = await context.newPage();
    await page.goto('/for-retailers');
    await expect(page.locator('h1')).toBeVisible();
    await expect(page.locator('.faq summary').first()).toBeVisible();
    await context.close();
  });

  test('no emoji in the rendered text', async ({ page }) => {
    for (const path of EN_PAGES) {
      await page.goto(path);
      const text = await page.locator('body').innerText();
      expect(text.replace(/[©®™]/g, ''), path).not.toMatch(/\p{Extended_Pictographic}/u);
    }
  });

  test('the home page claims no numbers the product rules do not hold', async ({ page }) => {
    await page.goto('/');
    const text = await page.locator('main').innerText();
    expect(text).not.toMatch(/\b\d+\s*%/);
    expect(text).not.toMatch(/\b\d[\d,]*\+/);
    expect(text).not.toMatch(/commission/i);
  });
});
