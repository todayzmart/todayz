import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef, stepperLabels } from '../test/fixtures';
import { QuantityStepper, type QuantityStepperProps } from './QuantityStepper';

function Stateful(props: QuantityStepperProps) {
  const [q, setQ] = useState(props.quantity);
  return <QuantityStepper {...props} quantity={q} onChange={setQ} />;
}

const meta = {
  title: 'Inputs and actions/QuantityStepper',
  component: QuantityStepper,
  args: { quantity: 0, labels: stepperLabels, itemName: 'Amul Taaza Milk', onChange: () => undefined },
  render: (args) => <Stateful {...args} />,
} satisfies Meta<typeof QuantityStepper>;

export default meta;
type Story = StoryObj<typeof meta>;

export const AddPill: Story = {};

export const Counting: Story = { args: { quantity: 2 } };

export const AtStockLimit: Story = { args: { quantity: 3, max: 3 } };

export const OutOfStock: Story = { args: { quantity: 0, max: 0 } };

export const Small: Story = { args: { quantity: 2, size: 'sm', alwaysStepper: true } };

export const Disabled: Story = { args: { quantity: 1, disabled: true } };

export const DesignReference: Story = {
  ...designRef('QuantityStepper', 72),
  render: () => (
    <div className="tz-row">
      <QuantityStepper quantity={0} onChange={() => undefined} labels={stepperLabels} />
      <QuantityStepper quantity={2} onChange={() => undefined} labels={stepperLabels} />
    </div>
  ),
};
