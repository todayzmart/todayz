import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { FilterButton } from './FilterButton';

describe('FilterButton', () => {
  it('is dashed when unset and shows the value when set', () => {
    const onClick = vi.fn();
    const { rerender } = render(<FilterButton label="Status" onClick={onClick} />);
    const b = screen.getByRole('button', { name: 'Status' });
    expect(b).toHaveAttribute('aria-haspopup', 'true');
    expect(b).not.toHaveAttribute('data-set');
    fireEvent.click(b);
    expect(onClick).toHaveBeenCalled();
    rerender(<FilterButton label="Status" value="Pending" />);
    expect(b).toHaveAttribute('data-set');
    expect(b).toHaveAccessibleName(/^Status\s*Pending$/);
    rerender(<FilterButton label="Status" value="" />);
    expect(b).not.toHaveAttribute('data-set');
  });
});
