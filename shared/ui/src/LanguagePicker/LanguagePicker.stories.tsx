import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef } from '../test/fixtures';
import { LanguagePicker, type LanguageOption } from './LanguagePicker';

const languages: readonly LanguageOption[] = [
  { code: 'en', native: 'English', name: 'English' },
  { code: 'hi', native: 'हिन्दी', name: 'Hindi' },
  { code: 'kn', native: 'ಕನ್ನಡ', name: 'Kannada' },
  { code: 'te', native: 'తెలుగు', name: 'Telugu' },
  { code: 'ta', native: 'தமிழ்', name: 'Tamil' },
];

function Picker({ initial }: { initial: string }) {
  const [value, setValue] = useState(initial);
  return <LanguagePicker label="Language" options={languages} value={value} onChange={setValue} />;
}

const meta = {
  title: 'Inputs and actions/LanguagePicker',
  component: Picker,
  args: { initial: 'en' },
  decorators: [(Story) => <div style={{ maxWidth: 520 }}>{Story()}</div>],
} satisfies Meta<typeof Picker>;

export default meta;
type Story = StoryObj<typeof meta>;

export const English: Story = {};

export const Kannada: Story = { args: { initial: 'kn' } };

export const DesignReference: Story = {
  ...designRef('LanguagePicker', 260),
  decorators: [],
  render: () => (
    <div style={{ padding: 'var(--space-4)', maxWidth: 520 }}>
      <LanguagePicker label="Language" options={languages} value="en" onChange={() => undefined} />
    </div>
  ),
};
