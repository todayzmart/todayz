import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { answerWith, problem, signIn } from './fake-api';

/**
 * A-22 (W3-15) against the built console: SC-OPS-01 and SC-OPS-03. admin and super_admin read the
 * page (locally the AWS panels say "Not configured (local)"); every other role gets 403 from the
 * fake like `admin.system.view` in ops.golden, has no sidebar link, and a URL visit shows no access.
 */

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

for (const role of ['admin', 'super_admin']) {
  test(`SC-OPS-01: ${role} reads system health, with the AWS panels not configured locally`, async ({ page }) => {
    await signIn(page, [role]);
    await page.goto('/');
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'System health', exact: true }).click();
    await expect(page).toHaveURL(/\/system$/);
    const main = page.getByRole('main');
    for (const name of ['Beanstalk app servers', 'Load balancer', 'RDS database server', 'CloudWatch alarms']) {
      await expect(main.getByRole('region', { name }).getByText('Not configured (local)')).toBeVisible();
    }
    await expect(main.getByRole('region', { name: 'Database' }).getByText('4 / 100')).toBeVisible();
    // Multi-AZ off is a fact locally (one server), not a warning.
    await expect(main.getByRole('region', { name: 'Database' }).getByRole('definition').filter({ hasText: /^Off$/ })).toBeVisible();
    await expect(main.getByRole('alert')).toHaveCount(0);
    await expect(main.getByRole('region', { name: 'Job queues' })).toBeVisible();
    await expect(main.getByRole('region', { name: 'Grafana dashboards' }).getByRole('link', { name: 'Jobs' })).toHaveAttribute('href', /^https:\/\/grafana\.todayz\.localhost\//);
    if (role === 'admin') await shot(page, 'admin-a22-system.png', { fullPage: true });
  });
}

for (const role of ['support', 'catalogue', 'analyst', 'finance', 'field_agent']) {
  test(`SC-OPS-03: ${role} has no System health link and a URL visit shows no access`, async ({ page }) => {
    await signIn(page, [role]);
    const asked: string[] = [];
    await answerWith(page, '/v1/admin/system', (_req, url) => {
      asked.push(url.pathname);
      return { status: 403, body: problem(403, 'forbidden') };
    });
    await page.goto('/system');
    const main = page.getByRole('main');
    await expect(main.getByRole('heading', { name: "You don't have access to this screen" })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'System health' })).toHaveCount(0);
    expect(asked, 'the page never asks for the summary').toEqual([]);
    if (role === 'support') await shot(page, 'admin-a22-no-access-support.png');
  });
}
