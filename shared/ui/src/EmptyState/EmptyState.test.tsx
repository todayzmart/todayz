import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { EmptyState } from './EmptyState';

describe('EmptyState', () => {
  it('renders the heading level, description, children and action', () => {
    render(
      <EmptyState
        title="No orders yet"
        description="New orders show here."
        headingLevel={2}
        action={<button type="button">Share your shop</button>}
      >
        <p>Tip: add 20 items first.</p>
      </EmptyState>,
    );
    expect(screen.getByRole('heading', { level: 2, name: 'No orders yet' })).toBeInTheDocument();
    expect(screen.getByText('New orders show here.')).toBeInTheDocument();
    expect(screen.getByText('Tip: add 20 items first.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Share your shop' })).toBeInTheDocument();
  });

  it('defaults to an h3 with the inbox icon and no description or action', () => {
    const { container } = render(<EmptyState title="Nothing here" />);
    expect(screen.getByRole('heading', { level: 3, name: 'Nothing here' })).toBeInTheDocument();
    expect(container.querySelector('.tz-iconring svg')).not.toBeNull();
    expect(container.querySelector('.tz-ui-empty-desc')).toBeNull();
    expect(container.querySelector('.tz-ui-empty-action')).toBeNull();
  });
});
