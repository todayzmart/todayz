import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CartBar } from './CartBar';

describe('CartBar', () => {
  it('announces the count label and total, hides the bare number and opens the cart', () => {
    const onOpen = vi.fn();
    const { container } = render(
      <CartBar
        count={3}
        countLabel="3 items"
        total="₹345"
        from="from Sri Lakshmi"
        ctaLabel="View cart"
        onOpen={onOpen}
      />,
    );
    const live = container.querySelector('[aria-live="polite"]');
    expect(live).toHaveTextContent('3 items, ₹345from Sri Lakshmi');
    expect(container.querySelector('.tz-cartbar-count')).toHaveAttribute('aria-hidden', 'true');
    fireEvent.click(screen.getByRole('button', { name: 'View cart' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('works without a shop line', () => {
    const { container } = render(
      <CartBar count={1} countLabel="1 item" total="₹45" ctaLabel="View cart" onOpen={() => undefined} />,
    );
    expect(container.querySelector('small')).toBeNull();
  });
});
