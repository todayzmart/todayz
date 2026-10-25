import type { AdminIntegration } from '@todayz/api-client/gen/model/adminIntegration';
import type { AdminNotificationTemplate } from '@todayz/api-client/gen/model/adminNotificationTemplate';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { previewParts } from './labels';
import { validateNotificationsSearch } from './search';

const text = (lang: AdminNotificationTemplate['texts'][number]['lang'], variant: string | null, body: string, fallback = false) => ({
  lang,
  variant,
  title: variant ? 'Order ready' : 'Order update',
  body,
  fallback,
});

const orderUpdate: AdminNotificationTemplate = {
  kind: 'order_update',
  app: 'customer',
  channels: ['push'],
  params: ['store', 'variant'],
  texts: [
    text('en', null, 'Your order from {store} has an update.'),
    text('en', 'ready', 'Your order at {store} is ready.'),
    text('hi', null, '{store} से आपके ऑर्डर में बदलाव है।'),
    text('hi', 'ready', 'Your order at {store} is ready.', true),
    ...(['kn', 'te', 'ta'] as const).flatMap((l) => [text(l, null, 'Your order from {store} has an update.', true), text(l, 'ready', 'Your order at {store} is ready.', true)]),
  ],
  providerTemplates: [],
  testable: true,
};

const orderUnseen: AdminNotificationTemplate = {
  kind: 'order_unseen',
  app: 'business',
  channels: ['sms', 'whatsapp'],
  params: ['code'],
  texts: [text('en', null, 'Order {code} is waiting.')],
  providerTemplates: [
    { lang: 'en', name: 'tz_order_unseen_en', smsTemplateIdSet: true },
    { lang: 'hi', name: 'tz_order_unseen_hi', smsTemplateIdSet: false },
  ],
  testable: false,
};

const integrations: AdminIntegration[] = [
  { channel: 'push', enabled: false, senderBuilt: false, vars: [{ name: 'VAPID_PRIVATE_KEY', set: false }], lastError: null },
  {
    channel: 'sms',
    enabled: true,
    senderBuilt: true,
    vars: [
      { name: 'SMS_API_URL', set: true },
      { name: 'SMS_SENDER_ID', set: false },
    ],
    lastError: { at: '2026-10-25T10:00:00Z', error: 'throttled' },
  },
  { channel: 'whatsapp', enabled: false, senderBuilt: false, vars: [{ name: 'WHATSAPP_API_KEY', set: false }], lastError: null },
];

function api(): Handler {
  return (c) => {
    if (c.path === '/v1/admin/notification-templates' && c.method === 'GET') {
      const app = new URLSearchParams(c.search).get('app');
      const items = [orderUnseen, orderUpdate].filter((t) => !app || t.app === app);
      return json(200, { items, serverNow: NOW });
    }
    if (c.path === '/v1/admin/integrations') return json(200, { items: integrations, serverNow: NOW });
    if (c.path === '/v1/admin/notification-templates/test-send') {
      const b = c.body as { kind: string; variant?: string | null };
      if (b.kind !== 'order_update') return problem(422, 'validation_failed');
      return json(201, { notificationId: '01JNOTIF000000000000000001', kind: b.kind, variant: b.variant ?? null, sentAt: NOW });
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('notifications search and preview', () => {
  it('keeps only well-formed filters', () => {
    expect(validateNotificationsSearch({ app: 'customer', kind: 'order_update', lang: 'hi', x: 1 })).toEqual({ app: 'customer', kind: 'order_update', lang: 'hi' });
    expect(validateNotificationsSearch({ app: 'shop', kind: 'Order Update', lang: 'en' })).toEqual({});
    expect(validateNotificationsSearch({ lang: 'fr' })).toEqual({});
  });

  it('cuts a text at its placeholders', () => {
    expect(previewParts('Your order at {store} is {variant}.')).toEqual([
      { text: 'Your order at ', param: false },
      { text: 'store', param: true },
      { text: ' is ', param: false },
      { text: 'variant', param: true },
      { text: '.', param: false },
    ]);
  });
});

describe('A-16 notifications', () => {
  it('shows each channel with its switch and keys as set or missing only', async () => {
    renderAt('/notifications', { roles: ['admin'], handlers: [api()] });
    const sms = await screen.findByRole('listitem', { name: 'SMS' });
    expect(within(sms).getByText('Switched on')).toBeInTheDocument();
    expect(within(sms).getByText('SMS_SENDER_ID')).toBeInTheDocument();
    expect(within(sms).getByText('Missing')).toBeInTheDocument();
    expect(within(sms).getByText(/^Last failure .*: throttled$/)).toBeInTheDocument();
    const push = screen.getByRole('listitem', { name: 'Push' });
    expect(within(push).getByText('Sender not built yet')).toBeInTheDocument();
    expect(within(push).getByText('No failed deliveries')).toBeInTheDocument();
  });

  it('filters by app in the URL and opens a kind with its texts per language', async () => {
    const { calls, router } = renderAt('/notifications', { roles: ['admin'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Notification kinds' });
    expect(await within(table).findByRole('button', { name: 'Order not seen' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('radio', { name: 'Customer' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ app: 'customer' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/notification-templates' && c.search === '?app=customer')).toBe(true));
    await waitFor(() => expect(within(table).queryByRole('button', { name: 'Order not seen' })).not.toBeInTheDocument());

    fireEvent.click(within(table).getByRole('button', { name: 'Order update' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ app: 'customer', kind: 'order_update' }));
    const card = await screen.findByRole('region', { name: 'Order update' });
    expect(within(card).getByRole('article', { name: 'ready' })).toHaveTextContent('Your order at store is ready.');
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();

    fireEvent.click(within(card).getByRole('tab', { name: 'Hindi' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ app: 'customer', kind: 'order_update', lang: 'hi' }));
    expect(within(within(card).getByRole('article', { name: 'ready' })).getByText('Shows English')).toBeInTheDocument();
    fireEvent.click(within(card).getByRole('tab', { name: 'Kannada' }));
    expect(await within(card).findByText('Kannada has no text of its own for this kind yet, so English is shown.')).toBeInTheDocument();
  });

  it('sends a test of a customer kind to your own inbox only', async () => {
    const { calls } = renderAt('/notifications?kind=order_update', { roles: ['admin'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'Order update' });
    expect(within(card).getByText(/Nothing goes by SMS, WhatsApp or push\./)).toBeInTheDocument();
    fireEvent.click(within(within(card).getByRole('article', { name: 'ready' })).getByRole('button', { name: 'Send a test to me' }));
    await waitFor(() => expect(screen.getByText('Test sent to your in-app inbox')).toBeInTheDocument());
    expect(calls.find((c) => c.path === '/v1/admin/notification-templates/test-send')?.body).toEqual({ kind: 'order_update', variant: 'ready' });
  });

  it('offers no test for a business kind and shows its DLT ids as set or missing', async () => {
    renderAt('/notifications?kind=order_unseen', { roles: ['admin'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'Order not seen' });
    expect(within(card).getByText('tz_order_unseen_hi')).toBeInTheDocument();
    expect(within(card).getByText('DLT id set')).toBeInTheDocument();
    expect(within(card).getByText('DLT id missing')).toBeInTheDocument();
    expect(within(card).queryByRole('button', { name: 'Send a test to me' })).not.toBeInTheDocument();
    expect(within(card).getByText(/cannot be sent as a test/)).toBeInTheDocument();
  });
});
