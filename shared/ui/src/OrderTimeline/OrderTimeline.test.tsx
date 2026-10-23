import { render, screen } from '@testing-library/react';
import { ShoppingBag } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { OrderTimeline } from './OrderTimeline';

const stateLabels = { done: 'done', upcoming: 'not yet', failed: 'did not happen' };

describe('OrderTimeline', () => {
  it('marks done, current, upcoming and failed steps for sight and screen readers', () => {
    render(
      <OrderTimeline
        label="Order progress"
        stateLabels={stateLabels}
        steps={[
          { key: 'placed', label: 'Order placed', state: 'done', detail: '5:02 pm' },
          { key: 'ready', label: 'Ready for pickup', state: 'now', icon: ShoppingBag },
          { key: 'collected', label: 'Collected', state: 'upcoming' },
          { key: 'refund', label: 'Refund', state: 'failed' },
        ]}
      />,
    );
    expect(screen.getByRole('list', { name: 'Order progress' })).toBeInTheDocument();
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveAttribute('data-done');
    expect(items[0]).toHaveTextContent('Order placed, done5:02 pm');
    expect(items[0]?.querySelector('svg')).not.toBeNull();
    expect(items[1]).toHaveAttribute('aria-current', 'step');
    expect(items[1]).toHaveAttribute('data-now');
    expect(items[1]).toHaveTextContent(/^Ready for pickup$/);
    expect(items[1]?.querySelector('svg')).not.toBeNull();
    expect(items[2]).toHaveTextContent('Collected, not yet');
    expect(items[2]?.querySelector('svg')).toBeNull();
    expect(items[3]).toHaveAttribute('data-failed');
    expect(items[3]).toHaveTextContent('Refund, did not happen');
  });

  it('the current step has no icon unless one is given', () => {
    render(
      <OrderTimeline
        label="Order progress"
        stateLabels={stateLabels}
        steps={[{ key: 'a', label: 'Accepted', state: 'now' }]}
      />,
    );
    expect(screen.getByRole('listitem').querySelector('svg')).toBeNull();
  });
});
