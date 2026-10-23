import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { orderStatusTone, type OrderStatus } from './orderStatus';
import { StatusPill } from './StatusPill';

describe('StatusPill', () => {
  it('neutral has no tone class; tones add tz-pill-<tone>', () => {
    const { rerender } = render(<StatusPill>Draft</StatusPill>);
    expect(screen.getByText('Draft').className).toBe('tz-pill');
    rerender(<StatusPill tone="ready">Ready for pickup</StatusPill>);
    expect(screen.getByText('Ready for pickup')).toHaveClass('tz-pill', 'tz-pill-ready');
  });
});

describe('orderStatusTone', () => {
  it('maps every order status to a pill tone', () => {
    const expected: Record<OrderStatus, string> = {
      placed: 'pending',
      awaiting_customer: 'pending',
      accepted: 'accepted',
      ready: 'ready',
      out_for_delivery: 'delivery',
      collected: 'completed',
      delivered: 'completed',
      rejected: 'rejected',
      delivery_failed: 'rejected',
      expired: 'cancelled',
      cancelled: 'cancelled',
    };
    for (const [status, tone] of Object.entries(expected)) {
      expect(orderStatusTone(status as OrderStatus)).toBe(tone);
    }
  });
});
