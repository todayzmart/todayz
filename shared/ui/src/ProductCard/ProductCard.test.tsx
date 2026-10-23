import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { stepperLabels, StoryLink } from '../test/fixtures';
import { ProductCard, type ProductCardProps } from './ProductCard';

function props(over: Partial<ProductCardProps> = {}): ProductCardProps {
  return {
    name: 'Aashirvaad Atta',
    unit: '5 kg',
    initials: 'AA',
    stock: { level: 'in', label: 'In stock', freshness: 'updated 12 min ago' },
    price: '₹289',
    mrp: '₹320',
    off: '10% off',
    quantity: 0,
    onQuantityChange: vi.fn(),
    stepperLabels,
    ...over,
  };
}

describe('ProductCard', () => {
  it('shows name, unit, stock, price with MRP and adds one', () => {
    const p = props();
    render(<ProductCard {...p} />);
    expect(screen.getByRole('article')).toHaveTextContent('Aashirvaad Atta');
    expect(screen.getByText('5 kg')).toBeInTheDocument();
    expect(screen.getByText('In stock')).toBeInTheDocument();
    expect(screen.getByText('₹320')).toHaveClass('tz-mrp');
    expect(screen.getByText('10% off')).toHaveClass('tz-off');
    expect(screen.getByText('AA')).toHaveAttribute('aria-hidden', 'true');
    fireEvent.click(screen.getByRole('button', { name: 'Add, Aashirvaad Atta' }));
    expect(p.onQuantityChange).toHaveBeenCalledWith(1);
  });

  it('links the name through the link component, shows the photo and keeps the row height without an MRP', () => {
    const { container } = render(
      <ProductCard
        {...props({
          mrp: undefined,
          unit: undefined,
          href: '/p/atta',
          LinkComponent: StoryLink,
          image: { src: '/a.webp' },
        })}
      />,
    );
    expect(screen.getByRole('link', { name: 'Aashirvaad Atta' })).toHaveAttribute('href', '/p/atta');
    expect(container.querySelector('img')).toHaveAttribute('alt', '');
    expect(container.querySelector('.tz-ui-mrp-spacer')).not.toBeNull();
    expect(container.querySelector('.tz-pcard-unit')).toBeNull();
    expect(screen.queryByText('AA')).toBeNull();
  });

  it('uses a plain anchor and the given alt text; an MRP without "off" shows alone', () => {
    const { container } = render(
      <ProductCard {...props({ href: '/p/atta', image: { src: '/a.webp', alt: 'Atta pack' }, off: undefined })} />,
    );
    expect(screen.getByRole('link', { name: 'Aashirvaad Atta' }).tagName).toBe('A');
    expect(screen.getByRole('img', { name: 'Atta pack' })).toBeInTheDocument();
    expect(container.querySelector('.tz-off')).toBeNull();
  });

  it('out of stock disables Add; an item already in the cart can still be reduced but not raised', () => {
    const { rerender } = render(<ProductCard {...props({ stock: { level: 'out', label: 'Out of stock' } })} />);
    expect(screen.getByRole('button', { name: 'Add, Aashirvaad Atta' })).toBeDisabled();
    rerender(<ProductCard {...props({ quantity: 2, stock: { level: 'out', label: 'Out of stock' } })} />);
    expect(screen.getByRole('button', { name: 'Remove one, Aashirvaad Atta' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Add one, Aashirvaad Atta' })).toBeDisabled();
  });
});
