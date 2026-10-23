import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Skeleton } from './Skeleton';

describe('Skeleton', () => {
  it('is a hidden text line by default', () => {
    const { container } = render(<Skeleton />);
    const el = container.firstChild as HTMLElement;
    expect(el).toHaveAttribute('aria-hidden', 'true');
    expect(el.className).toBe('tz-ui-skeleton tz-ui-skeleton-text');
    expect(el.style.width).toBe('100%');
    expect(el.style.height).toBe('1em');
  });

  it('takes a shape, size and class', () => {
    const { container } = render(<Skeleton shape="circle" width="40px" height="40px" className="x" />);
    const el = container.firstChild as HTMLElement;
    expect(el).toHaveClass('tz-ui-skeleton', 'tz-ui-skeleton-circle', 'x');
    expect(el.style.width).toBe('40px');
    expect(el.style.height).toBe('40px');
  });
});
