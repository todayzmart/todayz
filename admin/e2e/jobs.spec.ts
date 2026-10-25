import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, problem, signIn } from './fake-api';

/**
 * A-21 (W3-15) against the built console, with the admin-jobs API faked: SC-OPS-06 (badges, IST
 * schedule text, filters narrow the list) and SC-OPS-07 (who sees which buttons). The unit tests
 * cover the edge cases; this proves the bundle wires the screens, and leaves the screenshots.
 */

const RUN = '01JRVN0000000000000000R001';
const ADMIN = '01JADMIN0000000000000000AD';

const base = {
  owner: 'W3-07',
  scheduled: true,
  scheduleText: null,
  nextRunAt: '2026-10-26T00:30:00Z',
  lastRun: { id: RUN, status: 'succeeded', trigger: 'schedule', periodKey: '2026-10-25', createdAt: '2026-10-25T00:30:00Z', finishedAt: '2026-10-25T00:30:02Z' },
  successRate7dBp: 10000,
  avgDurationMs7d: 1200,
  maxDurationMs7d: 4100,
  runs7d: 7,
  health: 'ok',
  control: { paused: false, reason: null, updatedBy: null, updatedAt: null },
  runnable: true,
  pausable: true,
};

const KINDS = [
  { ...base, kind: 'notify_digest', class: 'notify', description: 'Sends the morning digest to opted-in customers.', schedule: '0 6 * * *' },
  {
    ...base,
    kind: 'order_hold_expiry',
    class: 'timer',
    owner: 'W1-11',
    description: 'Ends holds that ran out and frees the stock.',
    scheduled: false,
    schedule: null,
    nextRunAt: null,
    runnable: false,
    pausable: false,
  },
  {
    ...base,
    kind: 'payout_weekly',
    class: 'money',
    owner: 'W3-09',
    description: 'Builds the weekly payout batch.',
    schedule: '0 9 * * 1',
    health: 'failing',
    successRate7dBp: 5000,
    lastRun: { id: RUN, status: 'failed', trigger: 'schedule', periodKey: '2026-10-20', createdAt: '2026-10-20T03:30:00Z', finishedAt: '2026-10-20T03:30:09Z' },
  },
  { ...base, kind: 'stats_hourly', class: 'rollup', owner: 'W2-10', description: 'Rolls up hourly order stats.', schedule: '5 * * * *', health: 'missed' },
  {
    ...base,
    kind: 'banner_expiry',
    class: 'marketing',
    owner: 'W3-04',
    description: 'Ends banners whose time is up.',
    schedule: '*/15 * * * *',
    health: 'paused',
    nextRunAt: null,
    control: { paused: true, reason: 'Festival freeze', updatedBy: ADMIN, updatedAt: NOW },
  },
];

const run = (kind: string, cls: string) => ({
  id: RUN,
  kind,
  class: cls,
  trigger: 'schedule',
  periodKey: '2026-10-25',
  attempt: 2,
  status: 'failed',
  scheduledAt: '2026-10-25T00:30:00Z',
  startedAt: '2026-10-25T00:30:01Z',
  finishedAt: '2026-10-25T00:30:03Z',
  durationMs: 2100,
  latenessMs: 900,
  summary: { sent: 120, failed: 2 },
  traceId: '4bf92f3577b34da6a3ce929d0e0e4736',
  triggeredBy: null,
  createdAt: '2026-10-25T00:30:00Z',
});

/** The admin-jobs reads, filtered like the server (finance sees money kinds only); writes are refused. */
async function fakeJobs(page: Page, roles: string[]) {
  const finance = !roles.some((r) => r === 'admin' || r === 'super_admin' || r === 'support' || r === 'analyst');
  const posts: string[] = [];
  await answerWith(page, '/v1/admin/job**', (req, url) => {
    if (req.method() === 'POST') {
      posts.push(url.pathname);
      return { status: 403, body: problem(403, 'forbidden') };
    }
    if (url.pathname === '/v1/admin/jobs') {
      const cls = url.searchParams.get('class');
      const status = url.searchParams.get('status');
      const items = KINDS.filter((k) => (!finance || k.class === 'money') && (!cls || k.class === cls) && (!status || k.health === status));
      return { status: 200, body: { items, serverNow: NOW } };
    }
    const m = /^\/v1\/admin\/jobs\/([a-z0-9_]+)(\/runs)?$/.exec(url.pathname);
    const k = m && KINDS.find((x) => x.kind === m[1]);
    if (m && k) {
      if (m[2]) return { status: 200, body: { items: [run(k.kind, k.class), { ...run(k.kind, k.class), id: '01JRVN0000000000000000R000', status: 'succeeded', attempt: 1, summary: { sent: 118 } }], nextCursor: null, serverNow: NOW } };
      return {
        status: 200,
        body: { job: k, settings: { timeoutSeconds: 300, maxAttempts: 3, queue: 'default', concurrency: 1, batched: false, uniqueBy: 'day', runbook: 'docs/runbooks/jobs.md' }, serverNow: NOW },
      };
    }
    if (url.pathname === `/v1/admin/job-runs/${RUN}`) {
      return {
        status: 200,
        body: {
          run: run('notify_digest', 'notify'),
          note: 'Two numbers bounced; the rest were sent.',
          error: 'whatsapp send: 470 for phone [redacted]',
          attempts: [
            { id: '01JRVN0000000000000000R0A1', attempt: 1, status: 'failed', startedAt: '2026-10-25T00:30:01Z', finishedAt: '2026-10-25T00:30:02Z', error: 'timeout after 30 s' },
            { id: RUN, attempt: 2, status: 'failed', startedAt: '2026-10-25T00:30:02Z', finishedAt: '2026-10-25T00:30:03Z', error: null },
          ],
          explore: { logsQuery: `{service_name="todayz-worker"} | json | run_id="${RUN}"`, traceId: '4bf92f3577b34da6a3ce929d0e0e4736' },
          serverNow: NOW,
        },
      };
    }
    return { status: 404, body: problem(404, 'not_found') };
  });
  return posts;
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('SC-OPS-06: the list shows badges and IST schedules, and the filters narrow it', async ({ page }) => {
  await signIn(page, ['admin']);
  await fakeJobs(page, ['admin']);
  await page.goto('/jobs');
  const table = page.getByRole('table', { name: 'Jobs' });
  const row = (kind: string) => table.getByRole('row').filter({ has: page.getByRole('link', { name: kind, exact: true }) });
  await expect(row('notify_digest')).toContainText('Every day at 6:00 am');
  await expect(row('order_hold_expiry')).toContainText('On events');
  await expect(row('payout_weekly')).toContainText('Failing');
  await expect(row('stats_hourly')).toContainText('Missed');
  await expect(row('banner_expiry')).toContainText('Paused');
  await expect(table.getByRole('link')).toHaveCount(5);
  await shot(page, 'admin-a21-jobs.png', { fullPage: true });

  await page.getByLabel('Class').selectOption('money');
  await expect(page).toHaveURL(/class=money/);
  await expect(table.getByRole('link')).toHaveText(['payout_weekly']);
  await page.getByLabel('Class').selectOption('');
  await page.getByRole('radiogroup', { name: 'Status' }).getByRole('radio', { name: 'Missed' }).click();
  await expect(page).toHaveURL(/status=missed/);
  await expect(table.getByRole('link')).toHaveText(['stats_hourly']);
});

test('SC-OPS-07: admin runs and pauses an ordinary job and opens a run with Grafana links', async ({ page }) => {
  await signIn(page, ['admin']);
  await fakeJobs(page, ['admin']);
  await page.goto(`/jobs/notify_digest?run=${RUN}`);
  await expect(page.getByRole('heading', { level: 1, name: 'notify_digest' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run now' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(page.getByText('Two numbers bounced; the rest were sent.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Grafana logs' })).toHaveAttribute('href', /^https:\/\/grafana\.todayz\.localhost\/explore\?/);
  await expect(page.getByRole('link', { name: 'Grafana trace' })).toBeVisible();
  // Two audit panels, one per subject, each named for what it covers.
  await expect(page.getByRole('complementary', { name: 'Changes to this job' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Changes to this run' })).toBeVisible();
  await shot(page, 'admin-a21-kind.png', { fullPage: true });

  await page.getByRole('button', { name: 'Run now' }).click();
  const dialog = page.getByRole('dialog', { name: 'Run notify_digest now?' });
  await dialog.getByLabel('Period (optional)').fill('2026/10/24');
  await expect(dialog.getByText('Use the format 2026-10-22.')).toBeVisible();
  await shot(page, 'admin-a21-run-now.png');
});

test('SC-OPS-07: money jobs are super_admin only; admin sees why', async ({ page }) => {
  await signIn(page, ['admin']);
  await fakeJobs(page, ['admin']);
  await page.goto(`/jobs/payout_weekly?run=${RUN}`);
  await expect(page.getByText('Money jobs can be run, paused or retried by a super admin only, after a fresh authenticator code.')).toBeVisible();
  await expect(page.getByText('Two numbers bounced; the rest were sent.')).toBeVisible();
  for (const name of ['Run now', 'Pause', 'Retry']) await expect(page.getByRole('button', { name })).toHaveCount(0);
  await shot(page, 'admin-a21-money-admin.png');
});

test('SC-OPS-07: super_admin sees the money job buttons', async ({ page }) => {
  await signIn(page, ['super_admin']);
  await fakeJobs(page, ['super_admin']);
  await page.goto(`/jobs/payout_weekly?run=${RUN}`);
  await expect(page.getByRole('button', { name: 'Run now' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible();
});

test('SC-OPS-07: an order timer has no buttons, whoever looks', async ({ page }) => {
  await signIn(page, ['super_admin']);
  await fakeJobs(page, ['super_admin']);
  await page.goto('/jobs/order_hold_expiry');
  await expect(page.getByText("Order timers can't be paused or run by hand.")).toBeVisible();
  for (const name of ['Run now', 'Pause', 'Resume']) await expect(page.getByRole('button', { name })).toHaveCount(0);
  await shot(page, 'admin-a21-timer.png');
});

for (const role of ['support', 'analyst', 'finance']) {
  test(`SC-OPS-07: ${role} reads jobs with no action buttons`, async ({ page }) => {
    await signIn(page, [role]);
    const posts = await fakeJobs(page, [role]);
    const kind = role === 'finance' ? 'payout_weekly' : 'notify_digest';
    await page.goto('/jobs');
    await expect(page.getByRole('table', { name: 'Jobs' }).getByRole('link')).toHaveCount(role === 'finance' ? 1 : 5);
    await page.getByRole('link', { name: kind, exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: kind })).toBeVisible();
    await page.getByRole('table', { name: 'Run history' }).getByRole('button', { name: 'Failed' }).click();
    await expect(page.getByText('Two numbers bounced; the rest were sent.')).toBeVisible();
    for (const name of ['Run now', 'Pause', 'Resume', 'Retry']) await expect(page.getByRole('button', { name })).toHaveCount(0);
    await expect(page.getByText(/super admin only/)).toHaveCount(0);
    expect(posts).toEqual([]);
    if (role === 'support') await shot(page, 'admin-a21-support-readonly.png');
  });
}
