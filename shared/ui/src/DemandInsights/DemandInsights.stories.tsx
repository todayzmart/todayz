import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef, StoryLink, frame } from '../test/fixtures';
import { DemandInsights, type DemandInsightsProps } from './DemandInsights';
import type { InsightRow } from './insights';

const designRows: readonly InsightRow[] = [
  {
    id: 'selling_fast:amul-taaza',
    kind: 'selling_fast',
    label: 'Selling fast',
    sentence: (
      <>
        <strong>Amul Taaza Milk 500 ml</strong> sold 64 this week. At this pace you run out on Saturday.
      </>
    ),
    action: { label: 'Restock', href: '/stock/amul-taaza/restock' },
  },
  {
    id: 'sitting_on_shelf:kissan-jam',
    kind: 'sitting_on_shelf',
    label: 'Sitting on the shelf',
    sentence: (
      <>
        <strong>Kissan Mixed Fruit Jam 500 g</strong>: 12 in stock, no sale in 21 days.
      </>
    ),
    action: { label: 'Create offer', href: '/offers/new?product=kissan-jam' },
  },
  {
    id: 'going_to_waste:batch-1',
    kind: 'going_to_waste',
    label: 'Going to waste',
    sentence: (
      <>
        <strong>3 items expire within 5 days</strong>: Nandini Curd, Modern Bread, Milky Mist Paneer.
      </>
    ),
    action: { label: 'Flash offer', href: '/offers/new?flash=1' },
  },
  {
    id: 'wanted_nearby:nivea-lotion',
    kind: 'wanted_nearby',
    label: 'Wanted nearby',
    sentence: (
      <>
        <strong>Nivea Body Lotion 200 ml</strong>: 14 searches within 2 km this week. No shop nearby has it.
      </>
    ),
    action: { label: 'Add product', href: '/stock/new?q=nivea-body-lotion' },
  },
];

const expired: InsightRow = {
  id: 'expired_on_shelf:nandini-curd',
  kind: 'expired_on_shelf',
  label: 'Expired on the shelf',
  sentence: (
    <>
      <strong>Nandini Curd 400 g</strong>: 4 packs passed their expiry date yesterday. Take them off the shelf.
    </>
  ),
  action: { label: 'Mark removed', href: '/stock/nandini-curd/expired' },
};

const base: DemandInsightsProps = {
  title: 'Demand insights',
  description: 'What sells, what sits, what is about to go to waste, and what people nearby want',
  periodTag: 'Last 7 days',
  rows: designRows,
  limit: 4,
  allLink: { label: 'All insights', href: '/insights' },
  footNote: 'From your sales, stock and searches near your shop',
  empty: {
    title: 'Nothing needs you right now',
    checklist: (
      <ul>
        <li>Add cost prices so we can spot items selling below cost</li>
        <li>Add expiry dates to dairy and bakery items</li>
      </ul>
    ),
  },
  LinkComponent: StoryLink,
};

function Dismissable(props: DemandInsightsProps) {
  const [quiet, setQuiet] = useState<string[]>([]);
  return (
    <DemandInsights
      {...props}
      rows={props.rows.filter((r) => !quiet.includes(r.id))}
      notUsefulLabel="Not useful"
      onNotUseful={(r) => setQuiet((q) => [...q, r.id])}
    />
  );
}

const meta = {
  title: 'Dashboard/DemandInsights',
  component: DemandInsights,
  args: base,
  decorators: [frame({ maxWidth: 560 })],
} satisfies Meta<typeof DemandInsights>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithNotUseful: Story = { render: (args) => <Dismissable {...args} /> };

export const ExpiredFirst: Story = {
  args: { rows: [expired, ...designRows] },
  render: (args) => <Dismissable {...args} />,
};

export const Loading: Story = { args: { loading: true } };

export const NothingFired: Story = { args: { rows: [] } };

export const DesignReference: Story = {
  ...designRef('DemandInsights', 470, 620),
  render: (args) => (
    <div style={{ padding: 24, maxWidth: 560 }}>
      <DemandInsights {...args} />
    </div>
  ),
};
