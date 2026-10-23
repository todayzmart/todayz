import type { Meta, StoryObj } from '@storybook/react-vite';
import { Store, Tag, Truck } from 'lucide-react';
import { useState } from 'react';
import { designRef, radiusOptions } from '../test/fixtures';
import { Chip, ChipGroup, RadiusChips } from './Chip';

const meta = {
  title: 'Inputs and actions/Chip',
  component: Chip,
  args: { pressed: false, children: 'Open now' },
} satisfies Meta<typeof Chip>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Off: Story = {};

export const On: Story = { args: { pressed: true } };

export const WithIcon: Story = { args: { pressed: false, icon: Truck, children: 'Delivery' } };

export const Disabled: Story = { args: { disabled: true, children: 'Delivery' } };

function Radius() {
  const [km, setKm] = useState(2);
  return <RadiusChips label="Search radius" options={radiusOptions} value={km} onChange={setKm} />;
}

export const RadiusRow: Story = { render: () => <Radius /> };

function Filters() {
  const [value, setValue] = useState<string[]>(['open']);
  return (
    <ChipGroup<string>
      multiple
      label="Filters"
      value={value}
      onChange={setValue}
      options={[
        { value: 'open', label: 'Open now', icon: Store },
        { value: 'delivery', label: 'Delivery', icon: Truck },
        { value: 'offers', label: 'Offers', icon: Tag },
        { value: 'pharmacy', label: 'Pharmacy', disabled: true },
      ]}
    />
  );
}

export const MultipleChoice: Story = { render: () => <Filters /> };

const referenceChips: readonly [string, boolean][] = [
  ['1 km', false],
  ['2 km', true],
  ['5 km', false],
  ['10 km', false],
  ['Open now', true],
  ['Delivery', false],
  ['Offers', false],
];

export const DesignReference: Story = {
  ...designRef('Chip', 72),
  render: () => (
    <div className="tz-row">
      <div className="tz-chips">
        {referenceChips.map(([label, pressed]) => (
          <Chip key={label} pressed={pressed}>
            {label}
          </Chip>
        ))}
      </div>
    </div>
  ),
};
