import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { OtpInput, type OtpInputProps } from './OtpInput';

function Stateful(props: OtpInputProps) {
  const [value, setValue] = useState(props.value);
  return <OtpInput {...props} value={value} onChange={setValue} />;
}

const meta = {
  title: 'Inputs and actions/OtpInput',
  component: OtpInput,
  args: {
    label: "Enter customer's code",
    digitLabel: (i: number) => `Digit ${i}`,
    value: '',
    onChange: () => undefined,
    help: 'Order TZ-24817 · Ananya R.',
  },
  render: (args) => <Stateful {...args} />,
} satisfies Meta<typeof OtpInput>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {};

export const PartlyFilled: Story = { args: { value: '482' } };

export const WrongCode: Story = {
  args: { value: '4821', help: undefined, error: 'That code does not match. Check it with the customer.' },
};

export const SixDigitLogin: Story = {
  args: { label: 'Enter the 6-digit code', length: 6, help: 'Sent by SMS to +91 98450 12345' },
};

export const Disabled: Story = { args: { value: '4827', disabled: true } };
