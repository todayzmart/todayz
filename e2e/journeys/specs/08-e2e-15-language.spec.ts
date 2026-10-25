import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Page } from '@playwright/test';
import { Customer, cartSaved, customerFirstRun, expect, openApp, Shop, test, URLS } from '../../fixtures/journey';
import { STORES } from '../../fixtures/seed';

// The labels come from the catalogue itself (shared/i18n/messages/customer/<area>.<lc>.json), so a
// reviewer's wording change does not break the journey. W4-05 translated and the founder approved
// hi, kn, te and ta (docs/qa/W4-05-hindi-customer/REVIEW.md).
const MESSAGES = join(dirname(fileURLToPath(import.meta.url)), '../../../shared/i18n/messages/customer');
function msg(lc: string, key: string): string {
  const area = key.split('.')[0] ?? '';
  const file = JSON.parse(readFileSync(join(MESSAGES, `${area}.${lc}.json`), 'utf8')) as Record<string, { message: string }>;
  const entry = file[key];
  if (!entry) throw new Error(`${key} is not in ${area}.${lc}.json`);
  return entry.message;
}
const hi = (key: string) => msg('hi', key);
const te = (key: string) => msg('te', key);
/** The text before the first placeholder, without a trailing separator: "ऑर्डर करें · {total}" → "ऑर्डर करें". */
function lead(message: string): RegExp {
  const text = message.split('{')[0]?.replace(/[\s·]+$/u, '') ?? '';
  return new RegExp(`^${text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`);
}
const DEVANAGARI = /[\u0900-\u097F]/;

async function switchLanguage(page: Page, name: RegExp, lang: string) {
  await page.goto('/language');
  await page.getByRole('button', { name }).click();
  await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe(lang);
}

/** C-09 → C-11 → C-12 in Hindi: checks out the cart for pickup and returns the new order's id. */
async function checkoutCartInHindi(page: Page): Promise<string> {
  await cartSaved(page);
  if (new URL(page.url()).pathname !== '/cart') await page.goto('/cart');
  await expect(page.getByRole('heading', { level: 1, name: hi('cart.title') })).toBeVisible();
  await page.getByRole('button', { name: lead(hi('cart.checkout')) }).first().click();
  await expect(page.getByRole('heading', { level: 1, name: hi('checkout.title') })).toBeVisible();
  await page.getByRole('button', { name: lead(hi('checkout.place')) }).click();
  await expect(page.getByTestId('placed-check')).toBeVisible();
  await expect(page).toHaveURL(/\/orders\/[0-9A-Z]{26}$/);
  return new URL(page.url()).pathname.split('/').pop() ?? '';
}

// E2E-15 (builder-written): the customer switches language in the middle of a journey. The cart
// survives the switch, the page language and the stored choice follow it, the account's
// preferredLang is saved, and checkout carries on to a collected order.
test('E2E-15 language switch mid-journey keeps the cart and finishes the order', async ({ who, browser }) => {
  const asha = new Customer(await who('asha'));
  const shop = new Shop(await who('owner'), STORES.SL);
  const app = await openApp(browser, asha.who, URLS.customer);
  const { page } = app;
  try {
    await customerFirstRun(page);
    await test.step('put an item in the cart from search', async () => {
      await page.goto('/search?q=milk');
      await page.getByRole('button', { name: /^Add, / }).first().click();
      await cartSaved(page);
      await page.goto('/cart');
      await expect(page.getByRole('heading', { level: 1, name: 'Cart' })).toBeVisible();
      await expect(page.getByRole('button', { name: /Checkout/ }).first(), 'the cart holds the item').toBeVisible();
    });
    const cartText = await page.locator('main').innerText();

    await test.step('switch to Hindi on C-01', async () => {
      await page.goto('/language');
      await page.getByRole('button', { name: /हिन्दी/ }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe('hi');
      expect(await page.evaluate(() => localStorage.getItem('todayz.lang'))).toBe('hi');
      await expect.poll(async () => ((await asha.me()) as { preferredLang?: string }).preferredLang).toBe('hi');
    });

    await test.step('the cart is still there after the switch and a reload', async () => {
      await page.goto('/cart');
      await page.reload();
      await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe('hi');
      // The item name is shop data and stays as it was; "From {shop}" (cart.from) is translated since W4-05.
      const itemLine = cartText.split('\n').find((l) => /Nandini/.test(l)) ?? 'Nandini';
      await expect(page.locator('main').getByText(itemLine).first()).toBeVisible();
    });

    await test.step('checkout carries on in Hindi to a collected order', async () => {
      const id = await checkoutCartInHindi(page);
      const o = await asha.order(id);
      await shop.accept(id);
      await shop.ready(id);
      expect((await shop.handOver(id, await asha.code(id))).status).toBe('collected');
      expect(o.status).toBe('placed');
    });

    await test.step('switch back to English', async () => {
      await switchLanguage(page, /English/, 'en-IN'); // English keeps the en-IN tag (shared/i18n locales.ts)
    });
  } finally {
    await app.close();
  }
});

// W4-05: after the switch, Home (C-02), Cart (C-09) and Checkout (C-11) read in Hindi. Screenshots at
// 360 px are attached to the report (docs/qa/W4-05-hindi-customer/ keeps a copy).
test('E2E-15 the screens read in Hindi after the switch', async ({ who, browser }, testInfo) => {
  const asha = new Customer(await who('asha'));
  const app = await openApp(browser, asha.who, URLS.customer);
  const { page } = app;
  await page.setViewportSize({ width: 360, height: 780 });
  const shot = async (name: string) =>
    testInfo.attach(name, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' });
  try {
    await customerFirstRun(page);
    await page.goto('/search?q=milk');
    await page.getByRole('button', { name: /^Add, / }).first().click();
    await cartSaved(page);
    await switchLanguage(page, /हिन्दी/, 'hi');

    await test.step('Home', async () => {
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: hi('shell.hero.title') })).toBeVisible();
      const nav = page.getByRole('navigation', { name: hi('shell.nav.label') });
      await expect(nav.getByRole('link', { name: hi('shell.nav.home') })).toBeVisible();
      await expect(nav.getByRole('link', { name: hi('shell.nav.orders') })).toBeVisible();
      await expect(page.getByRole('heading', { level: 2, name: hi('home.shops.title') })).toBeVisible();
      await shot('hi-home-360');
    });

    await test.step('Cart', async () => {
      await page.goto('/cart');
      await expect(page.getByRole('heading', { level: 1, name: hi('cart.title') })).toBeVisible();
      await expect(page.getByRole('button', { name: lead(hi('cart.checkout')) }).first()).toBeVisible();
      expect(await page.locator('main').innerText(), 'the cart reads in Devanagari').toMatch(DEVANAGARI);
      await shot('hi-cart-360');
    });

    await test.step('Checkout', async () => {
      await page.getByRole('button', { name: lead(hi('cart.checkout')) }).first().click();
      await expect(page.getByRole('heading', { level: 1, name: hi('checkout.title') })).toBeVisible();
      await expect(page.getByRole('button', { name: lead(hi('checkout.place')) })).toBeVisible();
      await shot('hi-checkout-360');
    });
  } finally {
    // asha is shared with later journeys: leave the account in English.
    await switchLanguage(page, /English/, 'en-IN').catch(() => undefined);
    await app.close();
  }
});

// W4-05: a light check that another approved language loads too. Home (C-02) reads in Telugu.
test('E2E-15 Home reads in Telugu after the switch', async ({ who, browser }, testInfo) => {
  const asha = new Customer(await who('asha'));
  const app = await openApp(browser, asha.who, URLS.customer);
  const { page } = app;
  await page.setViewportSize({ width: 360, height: 780 });
  try {
    await customerFirstRun(page);
    await switchLanguage(page, /తెలుగు/, 'te');
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: te('shell.hero.title') })).toBeVisible();
    const nav = page.getByRole('navigation', { name: te('shell.nav.label') });
    await expect(nav.getByRole('link', { name: te('shell.nav.orders') })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: te('home.shops.title') })).toBeVisible();
    await testInfo.attach('te-home-360', { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' });
  } finally {
    await switchLanguage(page, /English/, 'en-IN').catch(() => undefined);
    await app.close();
  }
});
