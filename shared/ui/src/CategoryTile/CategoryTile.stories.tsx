import type { Meta, StoryObj } from '@storybook/react-vite';
import { Cpu, PenLine, Pill, ShoppingBasket, Sparkles, Wrench } from 'lucide-react';
import { designRef, StoryLink } from '../test/fixtures';
import { CategoryRow, CategoryTile, type CategoryRowProps } from './CategoryTile';

const categories: CategoryRowProps['categories'] = [
  { key: 'grocery', label: 'Grocery', icon: ShoppingBasket, tint: 'green', href: '/c/grocery' },
  { key: 'pharmacy', label: 'Pharmacy', icon: Pill, tint: 'rose', href: '/c/pharmacy' },
  { key: 'beauty', label: 'Beauty', icon: Sparkles, tint: 'lilac', href: '/c/beauty' },
  { key: 'electronics', label: 'Electronics', icon: Cpu, tint: 'sky', href: '/c/electronics' },
  { key: 'stationery', label: 'Stationery', icon: PenLine, tint: 'amber', href: '/c/stationery' },
  { key: 'hardware', label: 'Hardware', icon: Wrench, tint: 'peach', href: '/c/hardware' },
];

const meta = {
  title: 'Commerce/CategoryTile',
  component: CategoryTile,
  args: { label: 'Grocery', icon: ShoppingBasket, tint: 'green', href: '/c/grocery', LinkComponent: StoryLink },
  argTypes: { tint: { control: 'select', options: ['green', 'amber', 'rose', 'sky', 'lilac', 'peach'] } },
} satisfies Meta<typeof CategoryTile>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Single: Story = {};

export const Row: Story = {
  render: () => <CategoryRow label="Categories" categories={categories} LinkComponent={StoryLink} />,
};

export const DesignReference: Story = {
  ...designRef('CategoryTile', 140, 480),
  render: () => (
    <div style={{ padding: 24 }}>
      <CategoryRow label="Categories" categories={categories} LinkComponent={StoryLink} />
    </div>
  ),
};
