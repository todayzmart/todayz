import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef, stepperLabels, StoryLink, frame } from '../test/fixtures';
import { ProductCard, type ProductCardProps } from './ProductCard';

function Stateful(props: ProductCardProps) {
  const [q, setQ] = useState(props.quantity);
  return <ProductCard {...props} quantity={q} onQuantityChange={setQ} />;
}

const atta: ProductCardProps = {
  name: 'Aashirvaad Shudh Chakki Atta',
  unit: '5 kg',
  initials: 'AT',
  stock: { level: 'in', label: 'In stock', freshness: 'updated 12 min ago' },
  price: '₹289',
  mrp: '₹320',
  off: '10% off',
  quantity: 1,
  onQuantityChange: () => undefined,
  max: 12,
  stepperLabels,
  href: '/p/aashirvaad-atta-5kg',
  LinkComponent: StoryLink,
};

const milk: ProductCardProps = {
  name: 'Amul Taaza Toned Milk',
  unit: '500 ml',
  initials: 'AM',
  stock: { level: 'low', label: 'Only 3 left', freshness: 'updated 1 hr ago' },
  price: '₹28',
  quantity: 0,
  onQuantityChange: () => undefined,
  max: 3,
  stepperLabels,
};

const meta = {
  title: 'Commerce/ProductCard',
  component: ProductCard,
  args: atta,
  render: (args) => <Stateful {...args} />,
  decorators: [frame({ maxWidth: 184 })],
} satisfies Meta<typeof ProductCard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const InCart: Story = {};

export const NotInCart: Story = { args: milk };

export const LowStockAtLimit: Story = { args: { ...milk, quantity: 3 } };

export const OutOfStock: Story = {
  args: { ...milk, stock: { level: 'out', label: 'Out of stock', freshness: 'updated today, 9:15 am' } },
};

export const StaleStock: Story = {
  args: {
    ...atta,
    quantity: 0,
    stock: {
      level: 'in',
      label: 'In stock',
      freshness: 'updated 3 days ago',
      stale: true,
      staleLabel: 'Stock not confirmed recently',
    },
  },
};

export const LongName: Story = {
  args: {
    ...atta,
    name: 'Fortune Premium Kachi Ghani Pure Mustard Oil Pouch',
    unit: '1 L',
    initials: 'FO',
    quantity: 0,
  },
};

export const DesignReference: Story = {
  ...designRef('ProductCard', 360),
  render: () => (
    <div className="tz-grid2" style={{ padding: 'var(--space-4)', maxWidth: 400 }}>
      <ProductCard {...atta} href={undefined} />
      <ProductCard {...milk} />
    </div>
  ),
};
