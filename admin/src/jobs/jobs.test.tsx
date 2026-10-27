import type { JobKindSummary } from '@todayz/api-client/gen/model/jobKindSummary';
import type { JobRun } from '@todayz/api-client/gen/model/jobRun';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { STEP_UP_REQUEST, passStepUp, stepUpHandler } from '../test/step-up';
import { logsLink, traceLink } from './explore';
import { jobGate } from './gate';
import { scheduleLabel } from './schedule';
import { validateJobsSearch, validateKindSearch } from './search';

const RUN_ID = '01JRVN0000000000000000R001';
const OTHER_RUN = '01JRVN0000000000000000R002';
const ADMIN_ID = '01JADMIN0000000000000000AD';

function kind(over: Partial<JobKindSummary> = {}): JobKindSummary {
  return {
    kind: 'digest_daily',
    class: 'notify',
    owner: 'W3-07',
    description: 'Sends the morning digest.',
    scheduled: true,
    schedule: '0 6 * * *',
    scheduleText: 'Every day at 06:00 IST',
    nextRunAt: '2026-10-26T00:30:00Z',
    lastRun: { id: RUN_ID, status: 'succeeded', trigger: 'schedule', periodKey: '2026-10-25', createdAt: '2026-10-25T00:30:00Z', finishedAt: '2026-10-25T00:30:02Z' },
    successRate7dBp: 9850,
    avgDurationMs7d: 1200,
    maxDurationMs7d: 4100,
    runs7d: 7,
    health: 'ok',
    control: { paused: false, reason: null, updatedBy: null, updatedAt: null },
    runnable: true,
    pausable: true,
    ...over,
  };
}

const KINDS: JobKindSummary[] = [
  kind(),
  kind({
    kind: 'order_hold_expiry',
    class: 'timer',
    description: 'Ends holds that ran out.',
    scheduled: false,
    schedule: null,
    scheduleText: null,
    nextRunAt: null,
    runnable: false,
    pausable: false,
  }),
  kind({ kind: 'payout_weekly', class: 'money', description: 'Builds the weekly payout batch.', schedule: '0 9 * * 1', health: 'failing', lastRun: { id: OTHER_RUN, status: 'failed', trigger: 'schedule', periodKey: '2026-10-20', createdAt: '2026-10-20T03:30:00Z', finishedAt: '2026-10-20T03:30:09Z' } }),
  kind({ kind: 'rollup_hourly', class: 'rollup', description: 'Rolls up hourly stats.', schedule: '5 * * * *', health: 'missed' }),
  kind({ kind: 'banner_expiry', class: 'marketing', description: 'Ends banners.', health: 'paused', nextRunAt: null, control: { paused: true, reason: 'Festival freeze', updatedBy: ADMIN_ID, updatedAt: NOW } }),
];

function run(over: Partial<JobRun> = {}): JobRun {
  return {
    id: RUN_ID,
    kind: 'digest_daily',
    class: 'notify',
    trigger: 'schedule',
    periodKey: '2026-10-25',
    attempt: 1,
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
    ...over,
  };
}

/** The admin-jobs endpoints, filtering like the server (finance sees money kinds only). */
function api(opts: { roles?: string[]; stepUp?: boolean } = {}) {
  let kinds = KINDS;
  const state = { stepUpNeeded: opts.stepUp ?? false };
  const finance = opts.roles?.length === 1 && opts.roles[0] === 'finance';
  const handler: Handler = (c) => {
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    if (c.path === '/v1/admin/jobs' && c.method === 'GET') {
      const q = new URLSearchParams(c.search);
      const items = kinds.filter((k) => (!finance || k.class === 'money') && (!q.get('class') || k.class === q.get('class')) && (!q.get('status') || k.health === q.get('status')));
      return json(200, { items, serverNow: NOW });
    }
    const m = /^\/v1\/admin\/jobs\/([a-z0-9_]+)(\/runs|\/run|\/pause|\/resume)?$/.exec(c.path);
    if (m) {
      const k = kinds.find((x) => x.kind === m[1]);
      if (!k) return problem(404, 'not_found');
      if (!m[2]) return json(200, { job: k, settings: { timeoutSeconds: 60, maxAttempts: 3, queue: 'default', concurrency: 1, batched: false, uniqueBy: 'day', runbook: 'docs/runbooks/jobs.md' }, serverNow: NOW });
      if (m[2] === '/runs') {
        const q = new URLSearchParams(c.search);
        const items = [run({ kind: k.kind, class: k.class }), run({ id: OTHER_RUN, kind: k.kind, class: k.class, status: 'succeeded', trigger: 'manual', triggeredBy: ADMIN_ID })].filter(
          (r) => (!q.get('status') || r.status === q.get('status')) && (!q.get('trigger') || r.trigger === q.get('trigger')),
        );
        return json(200, { items, nextCursor: null, serverNow: NOW });
      }
      if (c.method !== 'POST') return undefined;
      if (state.stepUpNeeded && k.class === 'money') return problem(403, 'step_up_required');
      if (m[2] === '/run') return json(202, { runId: '01JRVN0000000000000000R003', kind: k.kind, trigger: 'manual', periodKey: (c.body as { periodKey?: string }).periodKey ?? '2026-10-25', serverNow: NOW });
      const paused = m[2] === '/pause';
      const control = { paused, reason: (c.body as { reason: string }).reason, updatedBy: ADMIN_ID, updatedAt: NOW };
      kinds = kinds.map((x) => (x.kind === k.kind ? { ...x, control, health: paused ? 'paused' : 'ok' } : x));
      return json(200, { kind: k.kind, control, serverNow: NOW });
    }
    const r = /^\/v1\/admin\/job-runs\/([0-9A-Z]{26})(\/retry)?$/.exec(c.path);
    if (r) {
      if (r[2]) {
        if (state.stepUpNeeded) return problem(403, 'step_up_required');
        return json(202, { runId: '01JRVN0000000000000000R004', kind: 'digest_daily', trigger: 'retry', periodKey: '2026-10-25', retryOf: r[1], serverNow: NOW });
      }
      return json(200, {
        run: run({ id: r[1] ?? RUN_ID }),
        note: 'Two numbers bounced.',
        error: 'send failed: phone [redacted]',
        attempts: [
          { id: RUN_ID, attempt: 1, status: 'failed', startedAt: '2026-10-25T00:30:01Z', finishedAt: '2026-10-25T00:30:03Z', error: 'timeout' },
        ],
        explore: { logsQuery: `{service_name="todayz-worker"} | run_id="${r[1]}"`, traceId: '4bf92f3577b34da6a3ce929d0e0e4736' },
        serverNow: NOW,
      });
    }
    return undefined;
  };
  return { handler, state };
}

describe('scheduleLabel', () => {
  it.each([
    ['0 6 * * *', 'Every day at 6:00 am'],
    ['30 0 * * *', 'Every day at 12:30 am'],
    ['5 13 * * *', 'Every day at 1:05 pm'],
    ['* * * * *', 'Every minute'],
    ['*/5 * * * *', 'Every 5 minutes'],
    ['15 * * * *', 'Every hour at :15'],
    ['0 */6 * * *', 'Every 6 hours at :00'],
    ['0 9 * * 1', 'Every Monday at 9:00 am'],
    ['0 9 * * 7', 'Every Sunday at 9:00 am'],
    ['0 2 1 * *', 'Monthly on day 1 at 2:00 am'],
  ])('%s → %s', (expr, text) => {
    expect(scheduleLabel(expr)).toBe(text);
  });

  it('falls back to the API text, then the expression', () => {
    expect(scheduleLabel('0 6 1 1 *', 'Once a year')).toBe('Once a year');
    expect(scheduleLabel('0 6 1-5 * *')).toBe('0 6 1-5 * * (IST)');
    expect(scheduleLabel(null)).toBe('');
  });
});

describe('Grafana Explore links', () => {
  const w = { createdAt: '2026-10-25T00:30:00Z', startedAt: '2026-10-25T00:30:01Z', finishedAt: '2026-10-25T00:30:03Z' };
  it('opens Loki and Tempo for the run window, five minutes either side', () => {
    const logs = logsLink('https://grafana.test', '{service_name="todayz-worker"}', w, NOW) ?? '';
    expect(logs.startsWith('https://grafana.test/explore?schemaVersion=1&orgId=1&panes=')).toBe(true);
    const panes = JSON.parse(decodeURIComponent(logs.split('panes=')[1] ?? '')) as { a: { datasource: string; queries: { expr: string }[]; range: { from: string; to: string } } };
    expect(panes.a.datasource).toBe('loki');
    expect(panes.a.queries[0]?.expr).toBe('{service_name="todayz-worker"}');
    expect(Number(panes.a.range.from)).toBe(Date.parse(w.startedAt) - 300_000);
    expect(Number(panes.a.range.to)).toBe(Date.parse(w.finishedAt) + 300_000);
    expect(traceLink('https://grafana.test', 'abc', w, NOW)).toContain(encodeURIComponent('"datasource":"tempo"'));
  });
  it('has no link without a Grafana URL or a trace id', () => {
    expect(logsLink('', 'x', w, NOW)).toBeNull();
    expect(traceLink('https://grafana.test', null, w, NOW)).toBeNull();
  });
});

describe('search validation', () => {
  it('keeps known values and drops the rest', () => {
    expect(validateJobsSearch({ class: 'money', status: 'failing' })).toEqual({ class: 'money', status: 'failing' });
    expect(validateJobsSearch({ class: 'nope', status: 'down' })).toEqual({});
    expect(validateKindSearch({ status: 'failed', trigger: 'manual', from: '2026-10-01', to: '2026-10-31', run: RUN_ID.toLowerCase() })).toEqual({
      status: 'failed',
      trigger: 'manual',
      from: '2026-10-01',
      to: '2026-10-31',
      run: RUN_ID,
    });
    expect(validateKindSearch({ status: 'x', from: '25-10-2026', run: 'abc' })).toEqual({});
  });
});

describe('jobGate (SC-OPS-07)', () => {
  it.each([
    [['super_admin'], 'notify', { timer: false, may: true, moneyLocked: false }],
    [['admin'], 'notify', { timer: false, may: true, moneyLocked: false }],
    [['super_admin'], 'money', { timer: false, may: true, moneyLocked: false }],
    [['admin'], 'money', { timer: false, may: false, moneyLocked: true }],
    [['finance'], 'money', { timer: false, may: false, moneyLocked: false }],
    [['support'], 'notify', { timer: false, may: false, moneyLocked: false }],
    [['analyst'], 'rollup', { timer: false, may: false, moneyLocked: false }],
    [['super_admin'], 'timer', { timer: true, may: false, moneyLocked: false }],
  ])('%j on %s', (roles, cls, want) => {
    expect(jobGate(roles, cls)).toEqual(want);
  });
});

describe('A-21 jobs list (SC-OPS-06)', () => {
  it('shows each kind with its IST schedule, last run, rate and badge', async () => {
    renderAt('/jobs', { roles: ['admin'], handlers: [api().handler] });
    const table = await screen.findByRole('table', { name: 'Jobs' });
    const daily = (await within(table).findByRole('link', { name: 'digest_daily' })).closest('tr') as HTMLElement;
    expect(within(daily).getByText('Every day at 6:00 am')).toBeInTheDocument();
    expect(within(daily).getByText('Notifications')).toBeInTheDocument();
    expect(within(daily).getByText('98.5%')).toBeInTheDocument();
    expect(within(daily).getByText('1.2 s / 4.1 s')).toBeInTheDocument();
    expect(within(daily).getByText('Succeeded')).toBeInTheDocument();
    const timer = within(table).getByRole('link', { name: 'order_hold_expiry' }).closest('tr') as HTMLElement;
    expect(within(timer).getByText('On events')).toBeInTheDocument();
    const row = (k: string) => within(table).getByRole('link', { name: k }).closest('tr') as HTMLElement;
    expect(within(row('payout_weekly')).getByText('Failing')).toBeInTheDocument();
    expect(within(row('payout_weekly')).getByText('Every Monday at 9:00 am')).toBeInTheDocument();
    expect(within(row('rollup_hourly')).getAllByText('Missed').length).toBeGreaterThan(0);
    expect(within(row('banner_expiry')).getByText('Paused')).toBeInTheDocument();
  });

  it('narrows the list by class and by badge, kept in the URL', async () => {
    const { calls, router } = renderAt('/jobs', { roles: ['admin'], handlers: [api().handler] });
    const table = await screen.findByRole('table', { name: 'Jobs' });
    expect(await within(table).findAllByRole('link')).toHaveLength(5);

    fireEvent.change(screen.getByLabelText('Class'), { target: { value: 'money' } });
    await waitFor(() => expect(within(screen.getByRole('table', { name: 'Jobs' })).getAllByRole('link')).toHaveLength(1));
    expect(router.state.location.search).toEqual({ class: 'money' });
    expect(calls.some((c) => c.path === '/v1/admin/jobs' && c.search === '?class=money')).toBe(true);

    fireEvent.change(screen.getByLabelText('Class'), { target: { value: '' } });
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    fireEvent.click(within(screen.getByRole('radiogroup', { name: 'Status' })).getByRole('radio', { name: 'Paused' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'paused' }));
    await waitFor(() => expect(within(screen.getByRole('table', { name: 'Jobs' })).getAllByRole('link').map((l) => l.textContent)).toEqual(['banner_expiry']));
  });

  it('says so when the filters match nothing', async () => {
    renderAt('/jobs?class=ai', { roles: ['admin'], handlers: [api().handler] });
    expect(await screen.findByText('No jobs match these filters.')).toBeInTheDocument();
  });
});

describe('A-21 kind page (SC-OPS-07)', () => {
  it('shows an order timer with no buttons and says why', async () => {
    renderAt('/jobs/order_hold_expiry', { roles: ['super_admin'], handlers: [api().handler] });
    expect(await screen.findByText("Order timers can't be paused or run by hand.")).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument();
  });

  it.each([['support'], ['analyst']])('shows %s the history with no action buttons', async (role) => {
    renderAt(`/jobs/digest_daily?run=${RUN_ID}`, { roles: [role], handlers: [api({ roles: [role] }).handler] });
    expect(await screen.findByRole('table', { name: 'Run history' })).toBeInTheDocument();
    expect(await screen.findByText('Two numbers bounced.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: /Changes/ })).not.toBeInTheDocument();
  });

  it('shows finance a money job read-only', async () => {
    renderAt(`/jobs/payout_weekly?run=${RUN_ID}`, { roles: ['finance'], handlers: [api({ roles: ['finance'] }).handler] });
    expect(await screen.findByText('Two numbers bounced.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
    expect(screen.queryByText(/super admin only/)).not.toBeInTheDocument();
  });

  it('tells an admin that money jobs are for a super admin', async () => {
    renderAt(`/jobs/payout_weekly?run=${RUN_ID}`, { roles: ['admin'], handlers: [api().handler] });
    expect(await screen.findByText('Money jobs can be run, paused or retried by a super admin only, after a fresh SMS code.')).toBeInTheDocument();
    expect(await screen.findByText('Two numbers bounced.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
  });

  it('runs a job now with a reason and a period key, then opens the new run', async () => {
    const { calls, router } = renderAt('/jobs/digest_daily', { roles: ['admin'], handlers: [api().handler] });
    expect(await screen.findByText('Every day at 6:00 am')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Run now' }));
    const dialog = await screen.findByRole('dialog', { name: 'Run digest_daily now?' });
    fireEvent.change(within(dialog).getByLabelText('Period (optional)'), { target: { value: '25-10-2026' } });
    expect(within(dialog).getByText('Use the format 2026-10-22.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Period (optional)'), { target: { value: '2026-10-24' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Digest did not go out' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Run now' }));

    await waitFor(() => expect(screen.getByText('Run started.')).toBeInTheDocument());
    const post = calls.find((c) => c.method === 'POST' && c.path === '/v1/admin/jobs/digest_daily/run');
    expect(post?.body).toEqual({ reason: 'Digest did not go out', periodKey: '2026-10-24' });
    expect(post?.headers.get('Idempotency-Key')).toBeTruthy();
    await waitFor(() => expect(router.state.location.search).toEqual({ run: '01JRVN0000000000000000R003' }));
  });

  it('pauses a job with a reason, and the page shows it paused', async () => {
    const { calls } = renderAt('/jobs/digest_daily', { roles: ['super_admin'], handlers: [api().handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    const dialog = await screen.findByRole('dialog', { name: 'Pause digest_daily?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Provider outage' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Pause' }));
    await waitFor(() => expect(screen.getByText('Job paused.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === '/v1/admin/jobs/digest_daily/pause')?.body).toEqual({ reason: 'Provider outage' });
    expect(await screen.findByRole('button', { name: 'Resume' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run now' })).not.toBeInTheDocument();
    expect(await screen.findByText('Provider outage')).toBeInTheDocument();
  });

  it('filters the run history by status and opens a run with its counts, error, attempts and Grafana links', async () => {
    const { router, calls } = renderAt('/jobs/digest_daily', { roles: ['admin'], handlers: [api().handler] });
    const history = await screen.findByRole('table', { name: 'Run history' });
    expect(await within(history).findByRole('button', { name: 'Failed' })).toBeInTheDocument();
    expect(within(history).getByRole('button', { name: 'Succeeded' })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'failed' } });
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'failed' }));
    expect(calls.some((c) => c.path === '/v1/admin/jobs/digest_daily/runs' && new URLSearchParams(c.search).get('status') === 'failed')).toBe(true);
    await waitFor(() => expect(within(screen.getByRole('table', { name: 'Run history' })).queryByRole('button', { name: 'Succeeded' })).not.toBeInTheDocument());

    fireEvent.click(within(screen.getByRole('table', { name: 'Run history' })).getByRole('button', { name: 'Failed' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'failed', run: RUN_ID }));
    expect(await screen.findByText('Two numbers bounced.')).toBeInTheDocument();
    expect(screen.getByText('send failed: phone [redacted]')).toBeInTheDocument();
    expect(screen.getByText('sent 120')).toBeInTheDocument();
    expect(screen.getByText('Attempt 1')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Grafana logs' }).getAttribute('href')).toContain('/explore?');
    expect(screen.getByRole('link', { name: 'Grafana trace' }).getAttribute('href')).toContain('tempo');
  });

  it('retries a failed money run after a fresh SMS code (super_admin)', async () => {
    const a = api({ stepUp: true });
    const { calls } = renderAt(`/jobs/payout_weekly?run=${RUN_ID}`, { roles: ['super_admin'], handlers: [a.handler, stepUpHandler(a.state)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    const dialog = await screen.findByRole('dialog', { name: 'Retry this run?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Bank was down' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Retry' }));

    await passStepUp();
    await waitFor(() => expect(screen.getByText('Retry started.')).toBeInTheDocument());
    const posts = calls.filter((c) => c.method === 'POST' && c.path === `/v1/admin/job-runs/${RUN_ID}/retry`);
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body).toEqual({ reason: 'Bank was down' });
    expect(calls.find((c) => c.path === '/v1/auth/step-up/verify')?.body).toEqual({ requestId: STEP_UP_REQUEST, code: '123456' });
  });
});
