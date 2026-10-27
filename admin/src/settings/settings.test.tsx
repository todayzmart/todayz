import type { AdminSetting } from '@todayz/api-client/gen/model/adminSetting';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { STEP_UP_REQUEST, sendStepUpCode, stepUpHandler, typeStepUpCode } from '../test/step-up';
import { parseInput, toInput } from './value';

function setting(over: Partial<AdminSetting>): AdminSetting {
  return {
    key: 'ask_max_stores',
    group: 'asks',
    unit: 'count',
    value: 5,
    min: 1,
    max: 20,
    description: 'Stores one ask goes to. 01 §15',
    money: false,
    superAdminOnly: false,
    editable: true,
    updatedBy: null,
    updatedByName: null,
    updatedAt: '2026-10-20T09:30:00Z',
    version: 1,
    ...over,
  };
}

const ROWS = (money: { editable: boolean }) => [
  setting({ key: 'credit_max_per_order', group: 'credits', unit: 'credits', value: 30, min: 0, max: 1000, money: true, superAdminOnly: true, ...money, description: 'Credits per order. 01 §7' }),
  setting({ key: 'delivery_fee_paise', group: 'orders', unit: 'paise', value: 2500, min: 0, max: 50_000, money: true, superAdminOnly: true, ...money, description: 'Delivery fee. 01 §5' }),
  setting({ key: 'hold_minutes', group: 'stock', unit: 'minutes', value: 30, min: 5, max: 240, description: 'Hold length. 01 §4' }),
  setting({ key: 'ask_max_stores' }),
];

interface Api {
  handler: Handler;
  /** PATCH answers step_up_required until a step-up verify succeeds. */
  stepUpNeeded: boolean;
}

/** The settings endpoints, answering like the server. */
function api(opts: { super?: boolean; stepUp?: boolean } = {}): Api {
  let items = ROWS({ editable: opts.super ?? false });
  const history: Record<string, unknown[]> = {};
  const state: Api = {
    stepUpNeeded: opts.stepUp ?? false,
    handler: (c) => {
      if (c.path === '/v1/admin/settings' && c.method === 'GET') {
        return json(200, { items, providers: [{ name: 'ANTHROPIC_API_KEY', set: false }, { name: 'WHATSAPP_API_KEY', set: true }], serverNow: NOW });
      }
      const m = /^\/v1\/admin\/settings\/([a-z0-9_]+)(\/history)?$/.exec(c.path);
      if (!m) return undefined;
      const key = m[1] ?? '';
      if (m[2]) return json(200, { items: history[key] ?? [], nextCursor: null, serverNow: NOW });
      if (c.method === 'PATCH') {
        if (state.stepUpNeeded) return problem(403, 'step_up_required');
        const body = c.body as { value: number; reason: string };
        const before = items.find((i) => i.key === key);
        if (!before) return problem(404, 'not_found');
        const after = { ...before, value: body.value, version: before.version + 1, updatedByName: 'Asha Rao', updatedAt: NOW };
        items = items.map((i) => (i.key === key ? after : i));
        history[key] = [
          { id: '01JB0A0D1A2B3C4D5E6F7G8H9J', actorId: null, actorName: 'Asha Rao', from: before.value, to: body.value, reason: body.reason, createdAt: NOW },
          ...(history[key] ?? []),
        ];
        return json(200, { setting: after, serverNow: NOW });
      }
      return undefined;
    },
  };
  return state;
}

describe('setting values', () => {
  it.each([
    ['paise', '25', 2500],
    ['paise', '12.5', 1250],
    ['paise', '12.05', 1205],
    ['bp', '2.5', 250],
    ['count', '9', 9],
  ] as const)('reads %s %s', (unit, raw, want) => expect(parseInput('k', unit, raw, { min: 0, max: 100_000 })).toEqual({ value: want }));

  it.each([
    ['paise', '12.555', 'number'],
    ['paise', '1e3', 'number'],
    ['count', '8.5', 'number'],
    ['count', '21', 'range'],
    ['count', '0', 'range'],
  ] as const)('refuses %s %s', (unit, raw, error) => expect(parseInput('k', unit, raw, { min: 1, max: 20 })).toEqual({ error }));

  it('keeps text trimmed and checks client versions', () => {
    expect(parseInput('min_client_version_customer', 'text', ' 1.4.0 ', { min: null, max: null })).toEqual({ value: '1.4.0' });
    expect(parseInput('min_client_version_customer', 'text', 'latest', { min: null, max: null })).toEqual({ error: 'version' });
    expect(parseInput('note', 'text', '', { min: null, max: null })).toEqual({ error: 'text' });
  });

  it('shows rupees and percent to two decimals', () => {
    expect(toInput('paise', 1250)).toBe('12.50');
    expect(toInput('paise', 2500)).toBe('25');
    expect(toInput('bp', 250)).toBe('2.50');
  });
});

describe('A-17 settings', () => {
  it('lists groups with values by unit, keeps money settings to super admins, and shows key states only', async () => {
    const a = api();
    renderAt('/settings', { roles: ['admin'], handlers: [a.handler] });
    expect(await screen.findByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument();
    const credits = screen.getByRole('table', { name: 'Credits and payouts' });
    expect(within(credits).getByText('30 credits')).toBeInTheDocument();
    expect(within(credits).getByText('Super admin only')).toBeInTheDocument();
    expect(within(screen.getByRole('table', { name: 'Orders' })).getByText('₹25')).toBeInTheDocument();
    expect(within(screen.getByRole('table', { name: 'Stock and holds' })).getByText('30 minutes')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Change ask_max_stores' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Change credit_max_per_order' })).not.toBeInTheDocument();
    const keys = screen.getByRole('region', { name: 'Provider keys' });
    expect(within(keys).getByText('ANTHROPIC_API_KEY')).toBeInTheDocument();
    expect(within(keys).getByText('Missing')).toBeInTheDocument();
    expect(within(keys).getByText('Set')).toBeInTheDocument();
  });

  it('keeps the admin sign-in limits to super admins (D-264)', async () => {
    const limit = setting({
      key: 'admin_signin_max_failures', group: 'access', unit: 'count', value: 10, min: 3, max: 50,
      superAdminOnly: true, editable: false, description: 'Admin sign-in. 01 §2',
    });
    const handler: Handler = (c) =>
      c.method === 'GET' && c.path === '/v1/admin/settings' ? json(200, { items: [limit], providers: [], serverNow: NOW }) : undefined;
    renderAt('/settings', { roles: ['admin'], handlers: [handler] });
    const access = await screen.findByRole('table', { name: 'Admin sign-in' });
    expect(within(access).getByText('Super admin only')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Change admin_signin_max_failures' })).not.toBeInTheDocument();
  });

  it('filters by group in the URL', async () => {
    const { router } = renderAt('/settings', { roles: ['admin'], handlers: [api().handler] });
    fireEvent.change(await screen.findByLabelText('Group'), { target: { value: 'stock' } });
    await waitFor(() => expect(router.state.location.search).toEqual({ group: 'stock' }));
    expect(screen.queryByRole('table', { name: 'Credits and payouts' })).not.toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Stock and holds' })).toBeInTheDocument();
  });

  it('checks the value against the range before calling the API', async () => {
    const { calls } = renderAt('/settings', { roles: ['admin'], handlers: [api().handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Change ask_max_stores' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change ask_max_stores' });
    fireEvent.change(within(dialog).getByLabelText('New value'), { target: { value: '21' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'more reach' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save change' }));
    expect(await within(dialog).findByText('Allowed: 1 to 20.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false);
  });

  it('asks for a fresh SMS code on a stale step-up, then saves the money change with its reason', async () => {
    const a = api({ super: true, stepUp: true });
    const { calls } = renderAt('/settings', { roles: ['super_admin'], handlers: [a.handler, stepUpHandler(a)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Change credit_max_per_order' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change credit_max_per_order' });
    expect(within(dialog).getByText('This is a money setting. It changes what customers pay or earn.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('New value'), { target: { value: '40' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save change' }));
    expect(await within(dialog).findByText('Write a reason, up to 500 characters.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Festival week' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save change' }));

    const step = await sendStepUpCode();
    expect(within(step).getByText('We sent a code by SMS to your staff number.')).toBeInTheDocument();
    expect(within(step).getByRole('button', { name: 'New code in 30 s' })).toBeDisabled();
    await typeStepUpCode('000000');
    expect(await within(step).findByText('That code is not right. 4 tries left.')).toBeInTheDocument();
    await typeStepUpCode('123456');

    expect(await screen.findByText('credit_max_per_order saved. Every server picks it up within moments.')).toBeInTheDocument();
    const patches = calls.filter((c) => c.method === 'PATCH');
    expect(patches).toHaveLength(2);
    expect(patches[1]?.body).toEqual({ value: 40, reason: 'Festival week' });
    expect(calls.filter((c) => c.path === '/v1/auth/step-up/verify').map((c) => c.body)).toEqual([
      { requestId: STEP_UP_REQUEST, code: '000000' },
      { requestId: STEP_UP_REQUEST, code: '123456' },
    ]);
    expect(calls.filter((c) => c.path === '/v1/auth/step-up/send')).toHaveLength(1);
    expect(within(screen.getByRole('table', { name: 'Credits and payouts' })).getByText('40 credits')).toBeInTheDocument();
    const panel = screen.getByRole('complementary', { name: 'Changes to credit_max_per_order' });
    expect(await within(panel).findByText('30 credits → 40 credits')).toBeInTheDocument();
    expect(within(panel).getByText('Festival week')).toBeInTheDocument();
  });

  it('enters rupees for a paise setting and sends paise', async () => {
    const a = api({ super: true });
    const { calls } = renderAt('/settings', { roles: ['super_admin'], handlers: [a.handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Change delivery_fee_paise' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change delivery_fee_paise' });
    expect(within(dialog).getByLabelText('New value')).toHaveValue('25');
    fireEvent.change(within(dialog).getByLabelText('New value'), { target: { value: '30.50' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'fuel' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save change' }));
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ value: 3050, reason: 'fuel' }));
    expect(await within(screen.getByRole('table', { name: 'Orders' })).findByText('₹30.50')).toBeInTheDocument();
  });
});
