import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { StockTag } from './StockTag';

describe('StockTag', () => {
  it('colours low and out, and shows the freshness inline', () => {
    const { container, rerender } = render(<StockTag level="in" label="In stock" freshness="updated 12 min ago" />);
    expect(screen.getByText('In stock').className).toBe('tz-stock');
    expect(container).toHaveTextContent('In stock · updated 12 min ago');
    rerender(<StockTag level="low" label="Only 3 left" />);
    expect(screen.getByText('Only 3 left')).toHaveClass('tz-stock-low');
    expect(container.querySelector('.tz-fresh')).toBeNull();
    rerender(<StockTag level="out" label="Out of stock" />);
    expect(screen.getByText('Out of stock')).toHaveClass('tz-stock-out');
  });

  it('stale stock shows the stale label instead of the freshness', () => {
    const { container } = render(
      <StockTag level="in" label="In stock" freshness="updated 3 days ago" stale staleLabel="Stock not confirmed" />,
    );
    expect(container.firstChild).toHaveAttribute('data-stale');
    expect(container).toHaveTextContent('Stock not confirmed');
    expect(container).not.toHaveTextContent('3 days');
  });

  it('stale without a stale label keeps the freshness; stacked puts it on its own line', () => {
    const { container } = render(
      <StockTag level="in" label="In stock" freshness="updated 3 days ago" stale layout="stacked" />,
    );
    expect(container.querySelector('div.tz-fresh')).toHaveTextContent('updated 3 days ago');
    const { container: bare } = render(<StockTag level="in" label="In stock" layout="stacked" />);
    expect(bare.querySelector('.tz-fresh')).toBeNull();
  });
});
