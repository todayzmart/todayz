import { render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { describe, expect, it } from 'vitest';
import { Banner } from '../Banner/Banner';
import { Chip } from '../Chip/Chip';
import { EmptyState } from '../EmptyState/EmptyState';
import { Skeleton } from '../Skeleton/Skeleton';
import { StatusPill } from '../StatusPill/StatusPill';

// Leaf components forward their ref and pass other attributes (test ids, data-*, aria-*) through.
describe('ref and rest forwarding', () => {
  it('StatusPill', () => {
    const ref = createRef<HTMLSpanElement>();
    render(
      <StatusPill ref={ref} tone="pending" data-testid="pill" title="Order status">
        Ready
      </StatusPill>,
    );
    expect(ref.current).toBe(screen.getByTestId('pill'));
    expect(ref.current).toHaveAttribute('title', 'Order status');
  });

  it('Skeleton stays decorative and keeps its size', () => {
    const ref = createRef<HTMLSpanElement>();
    render(<Skeleton ref={ref} width="6ch" data-testid="sk" style={{ margin: 2 }} aria-hidden={false} />);
    const sk = screen.getByTestId('sk');
    expect(ref.current).toBe(sk);
    expect(sk).toHaveAttribute('aria-hidden', 'true');
    expect(sk.style.width).toBe('6ch');
    expect(sk.style.margin).toBe('2px');
  });

  it('Banner', () => {
    const ref = createRef<HTMLDivElement>();
    render(
      <Banner ref={ref} title="You're offline." data-testid="banner" id="offline">
        Changes sync later.
      </Banner>,
    );
    expect(ref.current).toBe(screen.getByTestId('banner'));
    expect(ref.current).toHaveAttribute('id', 'offline');
  });

  it('EmptyState', () => {
    const ref = createRef<HTMLDivElement>();
    render(<EmptyState ref={ref} title="No orders yet" data-testid="empty" />);
    expect(ref.current).toBe(screen.getByTestId('empty'));
  });

  it('Chip', () => {
    const ref = createRef<HTMLButtonElement>();
    render(
      <Chip ref={ref} pressed={false}>
        Open now
      </Chip>,
    );
    expect(ref.current).toBe(screen.getByRole('button', { name: 'Open now' }));
  });
});
