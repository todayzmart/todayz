import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PickupCode } from './PickupCode';

describe('PickupCode', () => {
  it('reads the digits one by one and hides the visual number', () => {
    render(
      <PickupCode label="Pickup code" code="4827" note="Show this at the shop">
        <span>Held till 6:30 pm</span>
      </PickupCode>,
    );
    const group = screen.getByRole('group', { name: 'Pickup code' });
    expect(group).toHaveTextContent('4 8 2 7');
    expect(screen.getByText('4827')).toHaveAttribute('aria-hidden', 'true');
    expect(screen.getByText('Show this at the shop')).toBeInTheDocument();
    expect(screen.getByText('Held till 6:30 pm')).toBeInTheDocument();
  });

  it('has no group name when the label is not plain text, and no note when none is given', () => {
    const { container } = render(<PickupCode label={<b>Pickup code</b>} code="1234" />);
    expect(container.firstChild).not.toHaveAttribute('aria-label');
    expect(container.querySelector('.tz-ui-code-note')).toBeNull();
  });
});
