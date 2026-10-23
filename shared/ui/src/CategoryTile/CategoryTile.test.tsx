import { render, screen } from '@testing-library/react';
import { Milk, Pill } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { CategoryRow, CategoryTile } from './CategoryTile';

describe('CategoryTile', () => {
  it('is a tinted plain link named by its label', () => {
    render(<CategoryTile label="Dairy" icon={Milk} tint="sky" href="/c/dairy" />);
    const link = screen.getByRole('link', { name: 'Dairy' });
    expect(link).toHaveAttribute('data-tint', 'sky');
    expect(link).toHaveClass('tz-cat');
    expect(link.tagName).toBe('A');
  });
});

describe('CategoryRow', () => {
  it('is a named navigation of tiles', () => {
    render(
      <CategoryRow
        label="Categories"
        LinkComponent={StoryLink}
        categories={[
          { key: 'dairy', label: 'Dairy', icon: Milk, tint: 'sky', href: '/c/dairy' },
          { key: 'meds', label: 'Medicines', icon: Pill, tint: 'rose', href: '/c/meds' },
        ]}
      />,
    );
    expect(screen.getByRole('navigation', { name: 'Categories' })).toBeInTheDocument();
    expect(screen.getAllByRole('link').map((l) => l.getAttribute('href'))).toEqual(['/c/dairy', '/c/meds']);
  });
});
