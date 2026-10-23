import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef, stepperLabels, frame } from '../test/fixtures';
import { PosCart, type PosCartProps, type PosLine } from './PosCart';

const bill: readonly PosLine[] = [
  { id: 'atta', name: 'Aashirvaad Atta', detail: '5 kg · ₹289', quantity: 2, total: '₹578', max: 12 },
  { id: 'oil', name: 'Fortune Sunflower Oil', detail: '1 L · ₹165', quantity: 3, total: '₹495', max: 3 },
  { id: 'milk', name: 'Amul Taaza Milk', detail: '500 ml · ₹28', quantity: 6, total: '₹168' },
];

const paymentOptions = [
  { value: 'cash', label: 'Cash' },
  { value: 'upi', label: 'UPI' },
  { value: 'card', label: 'Card' },
  { value: 'credit', label: 'Credit' },
];

function cartProps(lines: readonly PosLine[], phone: string, method: string): PosCartProps {
  return {
    label: 'Current bill',
    customer: {
      label: 'Customer phone',
      placeholder: 'Customer phone (optional)',
      phone,
      onPhoneChange: () => undefined,
      summary: phone ? 'Ananya R. · 6 visits · 420 points' : undefined,
    },
    lines,
    onQuantityChange: () => undefined,
    stepperLabels,
    emptyLabel: 'Scan or search a product to start the bill.',
    payment: { label: 'Payment method', options: paymentOptions, value: method, onChange: () => undefined },
    summary: lines.length ? '11 items · ₹5 discount' : undefined,
    amount: lines.length ? '₹1,236' : '₹0',
    chargeLabel: 'Charge',
    onCharge: () => undefined,
  };
}

/** A working counter: quantities, phone and payment method change; prices are fixed demo data. */
function Counter(props: PosCartProps) {
  const [lines, setLines] = useState(props.lines);
  const [phone, setPhone] = useState(props.customer.phone);
  const [method, setMethod] = useState(props.payment.value);
  return (
    <PosCart
      {...props}
      customer={{
        ...props.customer,
        phone,
        onPhoneChange: setPhone,
        summary: phone.length >= 10 ? props.customer.summary : undefined,
      }}
      lines={lines}
      onQuantityChange={(id, q) =>
        setLines((ls) =>
          q <= 0 ? ls.filter((l) => l.id !== id) : ls.map((l) => (l.id === id ? { ...l, quantity: q } : l)),
        )
      }
      payment={{ ...props.payment, value: method, onChange: setMethod }}
    />
  );
}

const meta = {
  title: 'Commerce/PosCart',
  component: PosCart,
  args: cartProps(bill, '98450 14521', 'upi'),
  render: (args) => <Counter {...args} />,
  decorators: [frame({ maxWidth: 400 })],
} satisfies Meta<typeof PosCart>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Bill: Story = {};

export const WalkIn: Story = { args: cartProps(bill, '', 'cash') };

export const Empty: Story = { args: cartProps([], '', 'cash') };

export const Charging: Story = { args: { ...cartProps(bill, '98450 14521', 'upi'), charging: true } };

export const DesignReference: Story = {
  ...designRef('PosCart', 470),
  render: (args) => (
    <div style={{ padding: 'var(--space-4)', maxWidth: 400 }}>
      <PosCart {...args} />
    </div>
  ),
};
