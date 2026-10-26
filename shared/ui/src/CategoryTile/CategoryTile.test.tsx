import { fireEvent, render, screen } from '@testing-library/react';
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

describe('CategoryTile photo', () => {
  it('shows the photo in the icon\'s place, and the icon again when it fails', () => {
    const { container } = render(<CategoryTile label="Dairy" icon={Milk} tint="sky" href="/c/dairy" imageUrl="/p/dairy.webp" />);
    const img = container.querySelector('img');
    expect(img).toHaveAttribute('src', '/p/dairy.webp');
    expect(img).toHaveAttribute('alt', '');
    fireEvent.error(img as HTMLImageElement);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('svg')).not.toBeNull();
    expect(screen.getByRole('link', { name: 'Dairy' })).toBeInTheDocument();
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

  it('lays the tiles out as a grid when asked', () => {
    render(<CategoryRow label="Categories" layout="grid" categories={[{ key: 'dairy', label: 'Dairy', icon: Milk, tint: 'sky', href: '/c/dairy' }]} />);
    expect(screen.getByRole('navigation', { name: 'Categories' })).toHaveClass('tz-cats', 'tz-cats-grid');
  });
});
