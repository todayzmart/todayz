import type { AdminSystem } from '@todayz/api-client/gen/model/adminSystem';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW } from '../test/fixtures';
import { json, renderAt, type Handler } from '../test/render';

const OLD = '2026-10-25T06:29:15Z';

/** What `GET /v1/admin/system` answers locally: no AWS, the database and River read for real, Multi-AZ off (one server). */
function local(over: Partial<AdminSystem> = {}): AdminSystem {
  const off = { status: 'not_configured' as const, errorClass: null, fetchedAt: NOW };
  return {
    env: 'local',
    awsConfigured: false,
    beanstalk: { ...off, environments: [] },
    alb: { ...off, windowMinutes: 5, requestsPerMinute: null, http5xxRateBp: null, p95Ms: null },
    rds: { ...off, instances: [] },
    alarms: { ...off, items: [] },
    database: {
      status: 'ok',
      errorClass: null,
      fetchedAt: OLD,
      connections: 12,
      maxConnections: 100,
      sizeBytes: 3 * 1024 ** 3,
      multiAz: false,
      statementsStatus: 'ok',
      statements: [{ query: 'SELECT * FROM orders WHERE store_id = $1', calls: 1200, meanMs: 2.4, totalMs: 2880, rows: 9600 }],
    },
    river: { status: 'ok', errorClass: null, fetchedAt: OLD, running: 2, queues: [{ queue: 'default', depth: 3, oldestAgeSeconds: 14 }] },
    serverNow: NOW,
    ...over,
  };
}

function api(body: AdminSystem): Handler {
  return (c) => (c.path === '/v1/admin/system' && c.method === 'GET' ? json(200, body) : undefined);
}

const panel = (name: string) => screen.findByRole('region', { name });

describe('A-22 system health', () => {
  it('says the AWS panels are not configured locally and shows the database and queues', async () => {
    renderAt('/system', { roles: ['admin'], handlers: [api(local())] });
    for (const name of ['Beanstalk app servers', 'Load balancer', 'RDS database server', 'CloudWatch alarms']) {
      expect(within(await panel(name)).getByText('Not configured (local)')).toBeInTheDocument();
    }
    const db = await panel('Database');
    expect(within(db).getByText('45 s old')).toBeInTheDocument();
    expect(within(db).getByText('12 / 100')).toBeInTheDocument();
    // SC-OPS-01: off is said as a fact, not a warning.
    expect(within(db).getByText('Multi-AZ').nextElementSibling).toHaveTextContent('Off');
    expect(within(db).queryByRole('alert')).not.toBeInTheDocument();
    const sql = within(db).getByRole('table', { name: 'Top 10 SQL statements by total time' });
    expect(within(sql).getByText('SELECT * FROM orders WHERE store_id = $1')).toBeInTheDocument();
    expect(within(sql).getByText('1,200')).toBeInTheDocument();
    const river = await panel('Job queues');
    expect(within(river).getByText('2 jobs running')).toBeInTheDocument();
    expect(within(within(river).getByRole('table', { name: 'Job queues' })).getByText('14 s old')).toBeInTheDocument();
    const grafana = await panel('Grafana dashboards');
    expect(within(grafana).getByRole('link', { name: 'Jobs' })).toHaveAttribute('href', 'https://grafana.todayz.localhost/d/tz-jobs');
  });

  it('shows the AWS facts when configured, with Multi-AZ off as a fact', async () => {
    const at = '2026-10-25T06:28:00Z';
    renderAt('/system', {
      roles: ['super_admin'],
      handlers: [
        api(
          local({
            env: 'prod',
            awsConfigured: true,
            beanstalk: {
              status: 'ok',
              errorClass: null,
              fetchedAt: at,
              environments: [
                {
                  name: 'todayz-api-prod',
                  health: 'green',
                  status: 'Ready',
                  versionLabel: 'api-28d354c',
                  instanceCount: 2,
                  updatedAt: at,
                  deploys: [
                    { at, versionLabel: 'api-28d354c', outcome: 'succeeded' },
                    { at: '2026-10-24T06:00:00Z', versionLabel: 'api-eedc441', outcome: 'failed' },
                  ],
                },
              ],
            },
            alb: { status: 'ok', errorClass: null, fetchedAt: at, windowMinutes: 5, requestsPerMinute: 420, http5xxRateBp: 12, p95Ms: 180 },
            rds: {
              status: 'ok',
              errorClass: null,
              fetchedAt: at,
              instances: [
                { name: 'todayz-prod', status: 'available', instanceClass: 'db.t4g.medium', engineVersion: '16.4', multiAz: false, allocatedStorageGb: 50, cpuPercent: 18.5, connections: 22, freeStorageBytes: 40 * 1024 ** 3 },
              ],
            },
            alarms: { status: 'ok', errorClass: null, fetchedAt: at, items: [{ name: 'api-5xx-high', state: 'alarm', updatedAt: at }] },
          }),
        ),
      ],
    });
    const eb = await panel('Beanstalk app servers');
    expect(within(eb).getByText('2 min old')).toBeInTheDocument();
    expect(within(eb).getByText('Healthy')).toBeInTheDocument();
    expect(within(eb).getAllByText('api-28d354c').length).toBeGreaterThan(0);
    expect(within(eb).getByText('Failed')).toBeInTheDocument();
    const rds = await panel('RDS database server');
    expect(within(rds).getByText('Multi-AZ').nextElementSibling).toHaveTextContent('Off');
    expect(within(rds).getByText('40 GB / 50 GB')).toBeInTheDocument();
    expect(within(await panel('CloudWatch alarms')).getByText('In alarm')).toBeInTheDocument();
    expect(within(await panel('Load balancer')).getByText('180 ms')).toBeInTheDocument();
  });

  it('names the error class of a panel that could not load, and explains missing top statements', async () => {
    const base = local();
    renderAt('/system', {
      roles: ['admin'],
      handlers: [
        api({
          ...base,
          awsConfigured: true,
          rds: { ...base.rds, status: 'error', errorClass: 'AccessDenied' },
          database: { ...base.database, multiAz: null, statementsStatus: 'not_installed', statements: [] },
        }),
      ],
    });
    expect(within(within(await panel('RDS database server')).getByRole('alert')).getByText('(AccessDenied)')).toBeInTheDocument();
    const db = await panel('Database');
    expect(within(db).getByText('Top statements are off: the pg_stat_statements extension is not installed.')).toBeInTheDocument();
    // With RDS unreadable the API cannot say, so the row is left out.
    expect(within(db).queryByText('Multi-AZ')).not.toBeInTheDocument();
  });

  it('refreshes on Refresh', async () => {
    const { calls } = renderAt('/system', { roles: ['admin'], handlers: [api(local())] });
    await panel('Database');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh' })).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/system').length).toBe(2));
  });
});

describe('A-22 access (SC-OPS-01, SC-OPS-03)', () => {
  it.each([['admin'], ['super_admin']])('%s sees System health in the sidebar', async (role) => {
    renderAt('/system', { roles: [role], handlers: [api(local())] });
    const nav = within(await screen.findByRole('navigation', { name: 'Main' }));
    expect(await nav.findByRole('link', { name: 'System health' })).toBeInTheDocument();
  });

  it.each([['support'], ['catalogue'], ['analyst'], ['finance'], ['field_agent']])('%s has no System health link and a URL visit shows no access', async (role) => {
    const { calls } = renderAt('/system', { roles: [role], handlers: [api(local())] });
    expect(await screen.findByRole('heading', { name: "You don't have access to this screen" })).toBeInTheDocument();
    const nav = within(screen.getByRole('navigation', { name: 'Main' }));
    expect(nav.queryByRole('link', { name: 'System health' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/system')).toBe(false);
  });
});
