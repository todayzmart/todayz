import type { Meta, StoryObj } from '@storybook/react-vite';
import { designRef, StoryLink } from '../test/fixtures';
import { StoreCard, type StoreCardProps } from './StoreCard';

const lakshmi: StoreCardProps = {
  name: 'Sri Lakshmi Provision Stores',
  initials: 'SL',
  meta: ['0.6 km', 'Jayanagar 4th Block', '· 42 items match'],
  tags: [{ label: 'Open till 10 pm', kind: 'open' }, { label: 'Pickup' }, { label: 'Delivery' }],
  href: '/s/sri-lakshmi-provision-stores',
  LinkComponent: StoryLink,
};

const sharma: StoreCardProps = {
  name: 'Sharma General Store',
  initials: 'SG',
  meta: ['1.2 km', 'JP Nagar 2nd Phase'],
  tags: [{ label: 'Closed · opens 7 am' }, { label: '10% off on ₹500+', kind: 'offer' }],
  href: '/s/sharma-general-store',
  LinkComponent: StoryLink,
};

const meta = {
  title: 'Commerce/StoreCard',
  component: StoreCard,
  args: lakshmi,
  decorators: [(Story) => <div style={{ maxWidth: 420 }}>{Story()}</div>],
} satisfies Meta<typeof StoreCard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Open: Story = {};

export const ClosedWithOffer: Story = { args: sharma };

export const Tinted: Story = { args: { ...lakshmi, tint: 'amber' } };

export const NoTags: Story = { args: { ...sharma, tags: [] } };

export const LongName: Story = {
  args: { ...lakshmi, name: 'Sri Venkateshwara Medical and General Stores', initials: 'SV', tint: 'rose' },
};

export const DesignReference: Story = {
  ...designRef('StoreCard', 196),
  decorators: [],
  render: () => (
    <div className="tz-stack" style={{ maxWidth: 420 }}>
      <StoreCard {...lakshmi} />
      <StoreCard {...sharma} />
    </div>
  ),
};
