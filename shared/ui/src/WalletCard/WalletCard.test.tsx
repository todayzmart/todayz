import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { WalletCard } from './WalletCard';

describe('WalletCard', () => {
  it('is a region named by its label with points, worth and actions', () => {
    render(
      <WalletCard
        label="TodayZ points"
        points="420"
        worth="Worth ₹42"
        actions={<button type="button">Invite and earn 50</button>}
      />,
    );
    const region = screen.getByRole('region', { name: 'TodayZ points' });
    expect(region).toHaveTextContent('420');
    expect(screen.getByText('Worth ₹42')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Invite and earn 50' })).toBeInTheDocument();
  });

  it('works with points only', () => {
    const { container } = render(<WalletCard label="TodayZ points" points="0" />);
    expect(container.querySelectorAll('small')).toHaveLength(1);
    expect(container.querySelector('.tz-ui-wallet-actions')).toBeNull();
  });
});
