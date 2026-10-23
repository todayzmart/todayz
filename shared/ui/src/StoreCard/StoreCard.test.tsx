import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { StoreCard } from './StoreCard';

describe('StoreCard', () => {
  it('is one link holding the name, meta and tags', () => {
    render(
      <StoreCard
        name="Sri Lakshmi Provision Stores"
        initials="SL"
        tint="green"
        meta={['0.6 km', 'Jayanagar 4th Block']}
        tags={[{ label: 'Open till 10 pm', kind: 'open' }, { label: '10% off', kind: 'offer' }, { label: 'Pickup' }]}
        href="/s/sri-lakshmi"
        LinkComponent={StoryLink}
      />,
    );
    const link = screen.getByRole('link');
    expect(link).toHaveAttribute('href', '/s/sri-lakshmi');
    expect(link).toHaveTextContent('Sri Lakshmi Provision Stores');
    expect(link).toHaveTextContent('0.6 km');
    expect(screen.getByText('Open till 10 pm')).toHaveClass('tz-tag', 'tz-tag-open');
    expect(screen.getByText('10% off')).toHaveClass('tz-tag', 'tz-tag-offer');
    expect(screen.getByText('Pickup').className).toBe('tz-tag');
    expect(screen.getByText('SL')).toHaveAttribute('data-tint', 'green');
    expect(screen.getByText('SL')).toHaveAttribute('aria-hidden', 'true');
  });

  it('renders as a plain anchor with no tag row', () => {
    const { container } = render(<StoreCard name="Sharma General Store" initials="SG" meta={['1.2 km']} href="/s/x" />);
    expect(screen.getByRole('link').tagName).toBe('A');
    expect(container.querySelector('.tz-ui-meta-tags')).toBeNull();
  });
});
