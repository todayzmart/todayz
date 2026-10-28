import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, renderAt, type Handler } from '../test/render';

const messages = asMessages(en);
const BIZ = '/v1/biz/businesses';

const list =
  (invitations: unknown[]): Handler =>
  (c) =>
    c.method === 'GET' && c.path === BIZ ? json(200, { items: [], invitations, nextCursor: null }) : undefined;

describe('Gate: a signed-in person with no shop', () => {
  it('goes straight to B-02 instead of an empty dashboard, with a way to sign out', async () => {
    renderAt('/settings', { messages, handlers: [list([])] });
    expect(await screen.findByRole('heading', { name: 'Set up your shop' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Orders' })).not.toBeInTheDocument();
    expect(screen.queryByText('No shop yet')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
  });

  it('keeps the shell while an invitation waits: they are joining a shop, not setting one up', async () => {
    const invite = { staffId: 's1', businessName: 'Sri Lakshmi', role: 'cashier' };
    renderAt('/settings', { messages, handlers: [list([invite])] });
    expect(await screen.findByText('No shop yet')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Set up your shop' })).not.toBeInTheDocument();
  });

  it('keeps the shell when the list cannot be read (offline counter)', async () => {
    renderAt('/settings', { messages });
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Set up your shop' })).not.toBeInTheDocument();
  });
});
