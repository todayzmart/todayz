import { render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { describe, expect, it } from 'vitest';
import { StockTag } from './StockTag';

describe('StockTag', () => {
  it('colours low and out, and shows the freshness inline', () => {
    const { container, rerender } = render(<StockTag level="in" label="In stock" freshness="Confirmed 12 min ago" />);
    expect(screen.getByText('In stock').className).toBe('tz-stock');
    expect(container).toHaveTextContent('In stock · Confirmed 12 min ago');
    rerender(<StockTag level="low" label="Only 3 left" />);
    expect(screen.getByText('Only 3 left')).toHaveClass('tz-stock-low');
    expect(container.querySelector('.tz-fresh')).toBeNull();
    rerender(<StockTag level="out" label="Out of stock" />);
    expect(screen.getByText('Out of stock')).toHaveClass('tz-stock-out');
  });

  it('stale stock shows the stale label instead of the freshness', () => {
    const { container } = render(
      <StockTag level="in" label="In stock" freshness="Confirmed 3 days ago" stale staleLabel="Stock not confirmed recently" />,
    );
    expect(container.firstChild).toHaveAttribute('data-stale');
    expect(container).toHaveTextContent('Stock not confirmed recently');
    expect(container).not.toHaveTextContent('3 days');
  });

  it('stale never shows the old time, even without a stale label (untyped callers)', () => {
    const untyped = { stale: true, freshness: 'Confirmed 3 days ago' } as unknown as { stale: true; staleLabel: string };
    const { container } = render(<StockTag level="in" label="In stock" layout="stacked" {...untyped} />);
    expect(container).not.toHaveTextContent('3 days');
    expect(container.querySelector('.tz-fresh')).toBeNull();
  });

  it('stacked puts the freshness on its own line', () => {
    const { container } = render(
      <StockTag level="in" label="In stock" freshness="Confirmed 12 min ago" layout="stacked" />,
    );
    expect(container.querySelector('div.tz-fresh')).toHaveTextContent('Confirmed 12 min ago');
    const { container: bare } = render(<StockTag level="in" label="In stock" layout="stacked" />);
    expect(bare.querySelector('.tz-fresh')).toBeNull();
  });

  it('a stale tag must carry its stale label (types)', () => {
    // @ts-expect-error stale without staleLabel
    const bad = <StockTag level="in" label="In stock" stale freshness="Confirmed 3 days ago" />;
    expect(bad).toBeTruthy();
  });

  it('forwards ref and rest props', () => {
    const ref = createRef<HTMLDivElement>();
    render(<StockTag ref={ref} level="in" label="In stock" data-testid="tag" />);
    expect(ref.current).toBe(screen.getByTestId('tag'));
  });
});
